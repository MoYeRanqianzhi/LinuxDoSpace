package mailrelay

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	smtp "github.com/emersion/go-smtp"

	"linuxdospace/backend/internal/config"
)

const (
	// defaultForwardTimeout bounds one outbound forwarding attempt so an
	// unresponsive upstream SMTP relay cannot block the inbound session forever.
	defaultForwardTimeout = 60 * time.Second
)

// Server wraps the go-smtp listener used by LinuxDoSpace when the application
// itself receives and forwards email based on database-stored routes.
type Server struct {
	server *smtp.Server
}

// NewServer constructs the SMTP listener from runtime configuration, the
// database-backed recipient resolver, and the outbound SMTP forwarder.
func NewServer(mail config.MailConfig, resolver RecipientResolver, forwarder MessageForwarder, logger smtp.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}

	backend := &smtpBackend{
		resolver:       resolver,
		forwarder:      forwarder,
		logger:         logger,
		forwardTimeout: deriveForwardTimeout(mail),
	}

	server := smtp.NewServer(backend)
	server.Addr = strings.TrimSpace(mail.SMTPAddr)
	server.Domain = strings.TrimSpace(mail.Domain)
	server.ReadTimeout = mail.ReadTimeout
	server.WriteTimeout = mail.WriteTimeout
	server.MaxRecipients = mail.MaxRecipients
	server.MaxMessageBytes = mail.MaxMessageBytes
	server.ErrorLog = logger

	return &Server{server: server}
}

// ListenAndServe starts accepting inbound SMTP traffic.
func (s *Server) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the SMTP listener and waits for active sessions.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// smtpBackend creates one SMTP session per inbound TCP connection.
type smtpBackend struct {
	resolver       RecipientResolver
	forwarder      MessageForwarder
	logger         smtp.Logger
	forwardTimeout time.Duration
}

// NewSession allocates the per-connection SMTP session state.
func (b *smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{
		resolver:       b.resolver,
		forwarder:      b.forwarder,
		logger:         b.logger,
		forwardTimeout: b.forwardTimeout,
		recipients:     make([]ResolvedRecipient, 0, 4),
	}, nil
}

// smtpSession stores the current envelope state for one SMTP transaction.
type smtpSession struct {
	resolver       RecipientResolver
	forwarder      MessageForwarder
	logger         smtp.Logger
	forwardTimeout time.Duration
	mailFrom       string
	recipients     []ResolvedRecipient
}

// Mail stores the current transaction's envelope sender. The relay accepts the
// empty bounce sender `<>`, but rejects malformed values.
func (s *smtpSession) Mail(from string, opts *smtp.MailOptions) error {
	normalized, err := normalizeOptionalEnvelopeSender(from)
	if err != nil {
		return recipientSMTPError(err)
	}
	s.mailFrom = normalized
	s.recipients = s.recipients[:0]
	return nil
}

// Rcpt resolves one recipient against the local database before the message
// body is accepted, so unknown or disabled mailboxes fail early at RCPT time.
func (s *smtpSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	resolved, err := s.resolver.ResolveRecipient(context.Background(), to)
	if err != nil {
		return recipientSMTPError(err)
	}
	s.recipients = append(s.recipients, resolved)
	return nil
}

// Data reads the raw RFC 5322 message once and forwards it to the grouped
// target inboxes derived from the accepted recipients.
func (s *smtpSession) Data(r io.Reader) error {
	if len(s.recipients) == 0 {
		return &smtp.SMTPError{
			Code:         554,
			EnhancedCode: smtp.EnhancedCode{5, 5, 1},
			Message:      "no valid recipients were accepted",
		}
	}

	rawMessage, err := io.ReadAll(r)
	if err != nil {
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "failed to read smtp message body",
		}
	}

	for _, group := range groupRecipientsByTarget(s.recipients) {
		forwardCtx, cancel := context.WithTimeout(context.Background(), s.forwardTimeout)
		forwardErr := s.forwarder.Forward(forwardCtx, ForwardRequest{
			OriginalEnvelopeFrom: s.mailFrom,
			OriginalEnvelopeTo:   group.OriginalRecipients,
			TargetRecipients:     []string{group.TargetEmail},
			RawMessage:           rawMessage,
		})
		cancel()

		if forwardErr != nil {
			s.logger.Printf(
				"linuxdospace mail relay forward failed: target=%s recipients=%v err=%v",
				group.TargetEmail,
				group.OriginalRecipients,
				forwardErr,
			)
			return forwardSMTPError(forwardErr)
		}
	}

	return nil
}

// Reset discards the current transaction state as required by the SMTP session
// interface.
func (s *smtpSession) Reset() {
	s.mailFrom = ""
	s.recipients = s.recipients[:0]
}

// Logout releases session resources. This implementation keeps no extra state.
func (s *smtpSession) Logout() error {
	return nil
}

// groupedRecipients is the per-target message fan-out plan derived from the
// accepted inbound SMTP recipients.
type groupedRecipients struct {
	TargetEmail        string
	OriginalRecipients []string
}

// groupRecipientsByTarget collapses multiple inbound recipients that share the
// same resolved target inbox into one outbound forward action.
func groupRecipientsByTarget(recipients []ResolvedRecipient) []groupedRecipients {
	groups := make([]groupedRecipients, 0, len(recipients))
	indexByTarget := make(map[string]int, len(recipients))

	for _, item := range recipients {
		targetEmail := strings.ToLower(strings.TrimSpace(item.TargetEmail))
		if targetEmail == "" {
			continue
		}

		index, exists := indexByTarget[targetEmail]
		if !exists {
			index = len(groups)
			indexByTarget[targetEmail] = index
			groups = append(groups, groupedRecipients{
				TargetEmail:        targetEmail,
				OriginalRecipients: []string{item.OriginalRecipient},
			})
			continue
		}

		groups[index].OriginalRecipients = append(groups[index].OriginalRecipients, item.OriginalRecipient)
	}

	return groups
}

// normalizeOptionalEnvelopeSender accepts the empty sender used by SMTP bounces
// and otherwise reuses the same normalization rules as RCPT TO addresses.
func normalizeOptionalEnvelopeSender(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "<>" {
		return "", nil
	}

	localPart, domain, err := normalizeEnvelopeAddress(raw)
	if err != nil {
		return "", err
	}
	return localPart + "@" + domain, nil
}

// recipientSMTPError converts deterministic route-resolution failures into the
// explicit SMTP status codes returned at RCPT time.
func recipientSMTPError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidRecipient):
		return &smtp.SMTPError{
			Code:         553,
			EnhancedCode: smtp.EnhancedCode{5, 1, 3},
			Message:      "recipient address is invalid",
		}
	case errors.Is(err, ErrNoMatchingRoute):
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 1, 1},
			Message:      "recipient mailbox is not configured",
		}
	case errors.Is(err, ErrRouteDisabled), errors.Is(err, ErrRouteHasNoTarget):
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 2, 1},
			Message:      "recipient mailbox is unavailable",
		}
	case errors.Is(err, ErrTargetNotVerified), errors.Is(err, ErrTargetOwnershipMismatch):
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 7, 1},
			Message:      "recipient forwarding target is not available",
		}
	default:
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "temporary error while resolving recipient",
		}
	}
}

// forwardSMTPError converts message-forwarding failures into SMTP DATA errors.
func forwardSMTPError(err error) error {
	switch {
	case errors.Is(err, ErrRelayLoopDetected):
		return &smtp.SMTPError{
			Code:         554,
			EnhancedCode: smtp.EnhancedCode{5, 6, 0},
			Message:      "mail relay loop detected",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 1},
			Message:      "upstream smtp relay timed out",
		}
	default:
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "failed to forward message upstream",
		}
	}
}

// deriveForwardTimeout picks one sensible timeout for outbound forwarding
// attempts by reusing the configured SMTP read/write time budgets when present.
func deriveForwardTimeout(mail config.MailConfig) time.Duration {
	timeout := mail.ReadTimeout + mail.WriteTimeout
	if timeout <= 0 {
		return defaultForwardTimeout
	}
	return timeout
}
