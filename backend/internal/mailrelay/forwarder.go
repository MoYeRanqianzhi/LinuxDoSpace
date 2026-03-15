package mailrelay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	stdsmtp "net/smtp"
	"net/textproto"
	"sort"
	"strings"

	"linuxdospace/backend/internal/config"
)

const (
	// relayMarkerHeader is written to every forwarded message and rejected on
	// inbound mail so misconfigured routes cannot create infinite forward loops.
	relayMarkerHeader = "X-LinuxDoSpace-Relay"

	// originalEnvelopeFromHeader preserves the original SMTP MAIL FROM value
	// because the relay uses its own envelope sender when forwarding outward.
	originalEnvelopeFromHeader = "X-LinuxDoSpace-Original-Envelope-From"

	// originalEnvelopeToHeader records the accepted SMTP recipients that were
	// matched to one forwarded target inbox.
	originalEnvelopeToHeader = "X-LinuxDoSpace-Original-Envelope-To"
)

var (
	// ErrRelayLoopDetected means the incoming message already passed through the
	// LinuxDoSpace relay and must not be forwarded again.
	ErrRelayLoopDetected = errors.New("message already contains linuxdospace relay marker")
)

// MessageForwarder delivers one accepted SMTP message to its resolved target
// inboxes using the configured upstream SMTP relay.
type MessageForwarder interface {
	Forward(ctx context.Context, request ForwardRequest) error
}

// ForwardRequest is the normalized payload sent to the upstream SMTP relay.
type ForwardRequest struct {
	OriginalEnvelopeFrom string
	OriginalEnvelopeTo   []string
	TargetRecipients     []string
	RawMessage           []byte
}

// SMTPForwarder uses one configured upstream SMTP server to deliver the
// database-resolved mailbox routes to real inboxes.
type SMTPForwarder struct {
	addr     string
	username string
	password string
	from     string

	dialContext func(ctx context.Context, network string, address string) (net.Conn, error)
	lookupMX    func(ctx context.Context, name string) ([]*net.MX, error)
}

// NewSMTPForwarder builds the outbound forwarder from runtime configuration.
func NewSMTPForwarder(mail config.MailConfig) *SMTPForwarder {
	dialer := &net.Dialer{}
	return &SMTPForwarder{
		addr:        strings.TrimSpace(mail.ForwardHost),
		username:    strings.TrimSpace(mail.ForwardUsername),
		password:    strings.TrimSpace(mail.ForwardPassword),
		from:        strings.TrimSpace(mail.ForwardFrom),
		dialContext: dialer.DialContext,
		lookupMX:    net.DefaultResolver.LookupMX,
	}
}

// Forward writes loop-protection headers and sends the message to the resolved
// target inboxes through the configured upstream SMTP relay.
func (f *SMTPForwarder) Forward(ctx context.Context, request ForwardRequest) error {
	if len(request.TargetRecipients) == 0 {
		return fmt.Errorf("no target recipients were provided to the forwarder")
	}

	message, err := buildForwardMessage(request.RawMessage, request.OriginalEnvelopeFrom, request.OriginalEnvelopeTo)
	if err != nil {
		return err
	}

	return f.sendMessage(ctx, uniqueHeaderValues(request.TargetRecipients), message)
}

// buildForwardMessage validates the original message, blocks relay loops, and
// prepends the LinuxDoSpace-specific trace headers before forwarding.
func buildForwardMessage(raw []byte, originalEnvelopeFrom string, originalEnvelopeTo []string) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("smtp message body is empty")
	}

	header, err := parseMessageHeader(raw)
	if err != nil {
		return nil, fmt.Errorf("parse smtp message header: %w", err)
	}
	if strings.TrimSpace(header.Get(relayMarkerHeader)) != "" {
		return nil, ErrRelayLoopDetected
	}

	var builder strings.Builder
	builder.Grow(len(raw) + 256)
	builder.WriteString(relayMarkerHeader)
	builder.WriteString(": 1\r\n")
	builder.WriteString(originalEnvelopeFromHeader)
	builder.WriteString(": ")
	builder.WriteString(sanitizeHeaderValue(displayEnvelopeSender(originalEnvelopeFrom)))
	builder.WriteString("\r\n")
	builder.WriteString(originalEnvelopeToHeader)
	builder.WriteString(": ")
	builder.WriteString(sanitizeHeaderValue(strings.Join(uniqueHeaderValues(originalEnvelopeTo), ", ")))
	builder.WriteString("\r\n")

	message := append([]byte(builder.String()), raw...)
	return message, nil
}

// parseMessageHeader reads only the header section from one RFC 5322 message
// without mutating the original body. A malformed header is rejected because
// the relay would otherwise lose the ability to detect forwarding loops.
func parseMessageHeader(raw []byte) (textproto.MIMEHeader, error) {
	reader := textproto.NewReader(bufioReaderFromBytes(raw))
	header, err := reader.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	return header, nil
}

// bufioReaderFromBytes converts one raw message buffer into the buffered reader
// expected by net/textproto without copying the message body multiple times.
func bufioReaderFromBytes(raw []byte) *bufio.Reader {
	return bufio.NewReader(bytes.NewReader(raw))
}

// sendMessage selects the configured outbound delivery path. Deployments that
// provide MAIL_RELAY_FORWARD_HOST keep using one explicit upstream SMTP relay,
// while deployments without that variable fall back to direct per-domain MX
// delivery so the built-in relay can still forward mail after server migration.
func (f *SMTPForwarder) sendMessage(ctx context.Context, recipients []string, message []byte) error {
	if strings.TrimSpace(f.from) == "" {
		return fmt.Errorf("upstream smtp envelope sender is empty")
	}
	if strings.TrimSpace(f.addr) != "" {
		return f.sendMessageViaAddress(ctx, f.addr, recipients, message, true)
	}
	return f.sendMessageDirectly(ctx, recipients, message)
}

// sendMessageDirectly resolves one MX target set per recipient domain and sends
// the forwarded message straight to those remote mail exchangers on port 25.
func (f *SMTPForwarder) sendMessageDirectly(ctx context.Context, recipients []string, message []byte) error {
	recipientsByDomain, err := groupRecipientsByDomain(recipients)
	if err != nil {
		return err
	}

	failures := make([]string, 0)
	for domain, domainRecipients := range recipientsByDomain {
		targets, lookupErr := f.lookupDirectDeliveryTargets(ctx, domain)
		if lookupErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", domain, lookupErr))
			continue
		}

		var lastErr error
		for _, target := range targets {
			if err := f.sendMessageViaAddress(ctx, net.JoinHostPort(target, "25"), domainRecipients, message, false); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", domain, lastErr))
		}
	}

	if len(failures) != 0 {
		return fmt.Errorf("direct mx delivery failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

// lookupDirectDeliveryTargets resolves the remote MX hosts for one recipient
// domain and falls back to the bare domain when no explicit MX records exist.
func (f *SMTPForwarder) lookupDirectDeliveryTargets(ctx context.Context, domain string) ([]string, error) {
	normalizedDomain := strings.ToLower(strings.TrimSpace(domain))
	if normalizedDomain == "" {
		return nil, fmt.Errorf("recipient domain is empty")
	}
	if f.lookupMX == nil {
		return nil, fmt.Errorf("mx lookup function is not configured")
	}

	mxRecords, err := f.lookupMX(ctx, normalizedDomain)
	if err != nil {
		return nil, fmt.Errorf("lookup mx for %s: %w", normalizedDomain, err)
	}
	if len(mxRecords) == 0 {
		return []string{normalizedDomain}, nil
	}

	sort.SliceStable(mxRecords, func(left int, right int) bool {
		return mxRecords[left].Pref < mxRecords[right].Pref
	})

	targets := make([]string, 0, len(mxRecords))
	seen := make(map[string]struct{}, len(mxRecords))
	for _, item := range mxRecords {
		host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(item.Host)), ".")
		if host == "" {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		targets = append(targets, host)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("mx lookup for %s returned only empty hosts", normalizedDomain)
	}
	return targets, nil
}

// groupRecipientsByDomain keeps one SMTP transaction per remote recipient
// domain because direct MX delivery cannot mix recipients handled by different
// remote mail exchangers.
func groupRecipientsByDomain(recipients []string) (map[string][]string, error) {
	grouped := make(map[string][]string, len(recipients))
	for _, recipient := range recipients {
		normalizedRecipient := strings.ToLower(strings.TrimSpace(recipient))
		if normalizedRecipient == "" {
			continue
		}
		atIndex := strings.LastIndex(normalizedRecipient, "@")
		if atIndex <= 0 || atIndex == len(normalizedRecipient)-1 {
			return nil, fmt.Errorf("recipient %q is not a valid email address", recipient)
		}
		domain := strings.TrimSpace(normalizedRecipient[atIndex+1:])
		grouped[domain] = append(grouped[domain], normalizedRecipient)
	}
	return grouped, nil
}

// sendMessageViaAddress opens one SMTP client connection, upgrades it with
// STARTTLS when available, performs optional authentication, and sends the
// final message to one concrete remote SMTP server address.
func (f *SMTPForwarder) sendMessageViaAddress(ctx context.Context, address string, recipients []string, message []byte, allowAuth bool) error {
	remoteAddress := strings.TrimSpace(address)
	if remoteAddress == "" {
		return fmt.Errorf("upstream smtp address is empty")
	}

	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return fmt.Errorf("parse upstream smtp host %q: %w", remoteAddress, err)
	}

	dialContext := f.dialContext
	if dialContext == nil {
		dialer := &net.Dialer{}
		dialContext = dialer.DialContext
	}
	conn, err := dialContext(ctx, "tcp", remoteAddress)
	if err != nil {
		return fmt.Errorf("dial upstream smtp relay %s: %w", remoteAddress, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set upstream smtp deadline: %w", err)
		}
	}

	client, err := stdsmtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("create upstream smtp client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls with upstream smtp relay: %w", err)
		}
	}

	if allowAuth && (strings.TrimSpace(f.username) != "" || strings.TrimSpace(f.password) != "") {
		auth := stdsmtp.PlainAuth("", f.username, f.password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate with upstream smtp relay: %w", err)
		}
	}

	if err := client.Mail(f.from); err != nil {
		return fmt.Errorf("set upstream envelope sender %s: %w", f.from, err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set upstream recipient %s: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open upstream smtp data stream: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return fmt.Errorf("write message to upstream smtp relay: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize upstream smtp message: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit upstream smtp session: %w", err)
	}
	return nil
}

// displayEnvelopeSender renders the empty MAIL FROM as the visible `<>` bounce
// sender instead of leaving the forwarded header ambiguous.
func displayEnvelopeSender(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "<>"
	}
	return trimmed
}

// sanitizeHeaderValue removes CRLF so envelope-derived values cannot break out
// of the relay's own trace headers.
func sanitizeHeaderValue(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ")
	return replacer.Replace(strings.TrimSpace(value))
}

// uniqueHeaderValues removes duplicates while keeping the first-seen order so
// trace headers remain stable and readable.
func uniqueHeaderValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
