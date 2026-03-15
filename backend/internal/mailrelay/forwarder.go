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
	"sync"
	"time"

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

	// directSMTPPort is the standard SMTP port used when the relay connects
	// directly to the recipient domain's MX hosts.
	directSMTPPort = "25"
)

var (
	// ErrRelayLoopDetected means the incoming message already passed through the
	// LinuxDoSpace relay and must not be forwarded again.
	ErrRelayLoopDetected = errors.New("message already contains linuxdospace relay marker")
)

// MessageForwarder delivers one accepted SMTP message to its resolved target
// inboxes using direct MX delivery to the final recipient domains.
type MessageForwarder interface {
	Forward(ctx context.Context, request ForwardRequest) error
}

// ForwardRequest is the normalized payload sent to the outbound SMTP relay.
type ForwardRequest struct {
	OriginalEnvelopeFrom string
	OriginalEnvelopeTo   []string
	TargetRecipients     []string
	RawMessage           []byte
}

// SMTPForwarder uses direct MX delivery so LinuxDoSpace owns the full inbound
// and outbound SMTP path instead of depending on one external relay host.
type SMTPForwarder struct {
	from                 string
	helloDomain          string
	mxLookupTimeout      time.Duration
	mxCacheTTL           time.Duration
	maxDomainConcurrency int

	dialContext func(ctx context.Context, network string, address string) (net.Conn, error)
	lookupMX    func(ctx context.Context, name string) ([]*net.MX, error)
	now         func() time.Time

	mxCacheMu sync.Mutex
	mxCache   map[string]mxCacheEntry

	domainLimiter *domainConcurrencyLimiter
}

// mxCacheEntry stores one resolved target set together with its expiration time
// so hot domains do not trigger a DNS MX lookup on every delivery.
type mxCacheEntry struct {
	targets   []string
	expiresAt time.Time
}

// domainConcurrencyLimiter caps simultaneous deliveries to one recipient
// domain so bursts cannot flood the same remote MX cluster.
type domainConcurrencyLimiter struct {
	mu       sync.Mutex
	limit    int
	channels map[string]chan struct{}
}

// newDomainConcurrencyLimiter constructs the per-domain limiter used by the
// direct-MX forwarder.
func newDomainConcurrencyLimiter(limit int) *domainConcurrencyLimiter {
	return &domainConcurrencyLimiter{
		limit:    limit,
		channels: make(map[string]chan struct{}),
	}
}

// acquire blocks until the caller can start one more delivery to the target
// domain or the surrounding context expires.
func (l *domainConcurrencyLimiter) acquire(ctx context.Context, domain string) error {
	if l == nil || l.limit <= 0 {
		return nil
	}

	channel := l.channel(domain)
	select {
	case channel <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release frees one previously acquired domain-delivery slot.
func (l *domainConcurrencyLimiter) release(domain string) {
	if l == nil || l.limit <= 0 {
		return
	}

	channel := l.channel(domain)
	select {
	case <-channel:
	default:
	}
}

// channel returns the stable buffered semaphore channel for one domain.
func (l *domainConcurrencyLimiter) channel(domain string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	normalizedDomain := strings.ToLower(strings.TrimSpace(domain))
	if channel, exists := l.channels[normalizedDomain]; exists {
		return channel
	}

	channel := make(chan struct{}, l.limit)
	l.channels[normalizedDomain] = channel
	return channel
}

// NewSMTPForwarder builds the direct-MX outbound forwarder from runtime
// configuration. The returned instance is safe for concurrent worker use.
func NewSMTPForwarder(mail config.MailConfig) *SMTPForwarder {
	dialer := &net.Dialer{}
	return &SMTPForwarder{
		from:                 strings.TrimSpace(mail.ForwardFrom),
		helloDomain:          strings.TrimSpace(mail.HELODomain),
		mxLookupTimeout:      mail.MXLookupTimeout,
		mxCacheTTL:           mail.MXCacheTTL,
		maxDomainConcurrency: mail.MaxDomainConcurrency,
		dialContext:          dialer.DialContext,
		lookupMX:             net.DefaultResolver.LookupMX,
		now: func() time.Time {
			return time.Now().UTC()
		},
		mxCache:       make(map[string]mxCacheEntry),
		domainLimiter: newDomainConcurrencyLimiter(mail.MaxDomainConcurrency),
	}
}

// Forward writes loop-protection headers and sends the message to the resolved
// target inboxes by connecting directly to the recipient domains' MX hosts.
func (f *SMTPForwarder) Forward(ctx context.Context, request ForwardRequest) error {
	if len(request.TargetRecipients) == 0 {
		return fmt.Errorf("no target recipients were provided to the forwarder")
	}
	if strings.TrimSpace(f.from) == "" {
		return fmt.Errorf("smtp envelope sender is empty")
	}
	if strings.TrimSpace(f.helloDomain) == "" {
		return fmt.Errorf("smtp helo domain is empty")
	}

	message, err := buildForwardMessage(request.RawMessage, request.OriginalEnvelopeFrom, request.OriginalEnvelopeTo)
	if err != nil {
		return err
	}

	return f.sendMessageDirectly(ctx, uniqueHeaderValues(request.TargetRecipients), message)
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

// sendMessageDirectly resolves one MX target set per recipient domain and sends
// the forwarded message straight to those remote mail exchangers on port 25.
func (f *SMTPForwarder) sendMessageDirectly(ctx context.Context, recipients []string, message []byte) error {
	recipientsByDomain, err := groupRecipientsByDomain(recipients)
	if err != nil {
		return err
	}

	domains := make([]string, 0, len(recipientsByDomain))
	for domain := range recipientsByDomain {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	failures := make([]string, 0)
	for _, domain := range domains {
		if err := f.domainLimiter.acquire(ctx, domain); err != nil {
			failures = append(failures, fmt.Sprintf("%s: domain concurrency limit wait failed: %v", domain, err))
			continue
		}

		domainRecipients := recipientsByDomain[domain]
		targets, lookupErr := f.lookupDirectDeliveryTargets(ctx, domain)
		if lookupErr != nil {
			f.domainLimiter.release(domain)
			failures = append(failures, fmt.Sprintf("%s: %v", domain, lookupErr))
			continue
		}

		var lastErr error
		for _, target := range targets {
			if err := f.sendMessageViaAddress(ctx, net.JoinHostPort(target, directSMTPPort), domainRecipients, message); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		f.domainLimiter.release(domain)

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
	if cachedTargets, ok := f.loadCachedMXTargets(normalizedDomain); ok {
		return cachedTargets, nil
	}
	if f.lookupMX == nil {
		return nil, fmt.Errorf("mx lookup function is not configured")
	}

	lookupCtx, cancel := context.WithTimeout(ctx, f.mxLookupTimeout)
	defer cancel()

	mxRecords, err := f.lookupMX(lookupCtx, normalizedDomain)
	if err != nil {
		return nil, fmt.Errorf("lookup mx for %s: %w", normalizedDomain, err)
	}
	if len(mxRecords) == 0 {
		targets := []string{normalizedDomain}
		f.storeCachedMXTargets(normalizedDomain, targets)
		return targets, nil
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

	f.storeCachedMXTargets(normalizedDomain, targets)
	return copyStringSlice(targets), nil
}

// loadCachedMXTargets returns one still-valid MX target set if the domain was
// looked up recently enough to remain inside the configured cache window.
func (f *SMTPForwarder) loadCachedMXTargets(domain string) ([]string, bool) {
	if f == nil || f.mxCacheTTL <= 0 {
		return nil, false
	}

	f.mxCacheMu.Lock()
	defer f.mxCacheMu.Unlock()

	entry, exists := f.mxCache[domain]
	if !exists {
		return nil, false
	}
	if f.now().After(entry.expiresAt) {
		delete(f.mxCache, domain)
		return nil, false
	}
	return copyStringSlice(entry.targets), true
}

// storeCachedMXTargets writes one resolved MX target set into the in-memory
// cache so repeated deliveries to hot domains avoid extra DNS load.
func (f *SMTPForwarder) storeCachedMXTargets(domain string, targets []string) {
	if f == nil || f.mxCacheTTL <= 0 {
		return
	}

	f.mxCacheMu.Lock()
	defer f.mxCacheMu.Unlock()

	f.mxCache[domain] = mxCacheEntry{
		targets:   copyStringSlice(targets),
		expiresAt: f.now().Add(f.mxCacheTTL),
	}
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
// STARTTLS when available, and sends the final message to one concrete remote
// SMTP server address.
func (f *SMTPForwarder) sendMessageViaAddress(ctx context.Context, address string, recipients []string, message []byte) error {
	remoteAddress := strings.TrimSpace(address)
	if remoteAddress == "" {
		return fmt.Errorf("smtp address is empty")
	}

	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return fmt.Errorf("parse smtp host %q: %w", remoteAddress, err)
	}

	dialContext := f.dialContext
	if dialContext == nil {
		dialer := &net.Dialer{}
		dialContext = dialer.DialContext
	}
	conn, err := dialContext(ctx, "tcp", remoteAddress)
	if err != nil {
		return fmt.Errorf("dial smtp server %s: %w", remoteAddress, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set smtp deadline: %w", err)
		}
	}

	client, err := stdsmtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if err := client.Hello(f.helloDomain); err != nil {
		return fmt.Errorf("announce smtp helo %s: %w", f.helloDomain, err)
	}

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls with smtp server: %w", err)
		}
	}

	if err := client.Mail(f.from); err != nil {
		return fmt.Errorf("set smtp envelope sender %s: %w", f.from, err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set smtp recipient %s: %w", recipient, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open smtp data stream: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return fmt.Errorf("write message to smtp server: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize smtp message: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit smtp session: %w", err)
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

// copyStringSlice returns one detached copy so cached MX targets cannot be
// mutated by callers after they are loaded.
func copyStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}
