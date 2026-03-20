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
	// unresponsive remote MX host cannot block one queue worker forever.
	defaultForwardTimeout = 60 * time.Second
)

// Server wraps the go-smtp listener used by LinuxDoSpace when the application
// itself receives and forwards email based on database-stored routes.
type Server struct {
	server *smtp.Server
}

// NewServer constructs the SMTP listener from runtime configuration, the
// database-backed recipient resolver, and the durable delivery queue.
func NewServer(mail config.MailConfig, resolver RecipientResolver, queueStore QueueStore, tokenHub *TokenStreamHub, logger smtp.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}

	backend := &smtpBackend{
		resolver:             resolver,
		queue:                NewPersistentQueue(mail, queueStore, tokenHub),
		logger:               logger,
		resolveTimeout:       mail.ResolveTimeout,
		enqueueTimeout:       mail.EnqueueTimeout,
		maxConcurrentIngress: mail.MaxConcurrentIngress,
		ingressSlots:         make(chan struct{}, mail.MaxConcurrentIngress),
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
	resolver             RecipientResolver
	queue                DeliveryQueue
	logger               smtp.Logger
	resolveTimeout       time.Duration
	enqueueTimeout       time.Duration
	maxConcurrentIngress int
	ingressSlots         chan struct{}
}

// NewSession allocates the per-connection SMTP session state.
func (b *smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{
		resolver:       b.resolver,
		queue:          b.queue,
		logger:         b.logger,
		resolveTimeout: b.resolveTimeout,
		enqueueTimeout: b.enqueueTimeout,
		ingressSlots:   b.ingressSlots,
		recipients:     make([]ResolvedRecipient, 0, 4),
	}, nil
}

// smtpSession stores the current envelope state for one SMTP transaction.
type smtpSession struct {
	resolver       RecipientResolver
	queue          DeliveryQueue
	logger         smtp.Logger
	resolveTimeout time.Duration
	enqueueTimeout time.Duration
	ingressSlots   chan struct{}
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
	resolveCtx, cancel := context.WithTimeout(context.Background(), s.resolveTimeout)
	defer cancel()

	resolved, err := s.resolver.ResolveRecipient(resolveCtx, to)
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

	if !s.acquireIngressSlot() {
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 2},
			Message:      "mail relay is busy, please retry later",
		}
	}
	defer s.releaseIngressSlot()

	rawMessage, err := io.ReadAll(r)
	if err != nil {
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "failed to read smtp message body",
		}
	}

	enqueueCtx, cancel := context.WithTimeout(context.Background(), s.enqueueTimeout)
	defer cancel()

	if err := s.queue.Enqueue(enqueueCtx, EnqueueRequest{
		OriginalEnvelopeFrom: s.mailFrom,
		RawMessage:           rawMessage,
		Groups:               groupRecipientsByTarget(s.recipients),
	}); err != nil {
		return queueSMTPError(err)
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
	TargetKind           string
	TargetEmail          string
	TargetTokenPublicID  string
	OriginalRecipients   []string
	OwnerUserIDs         []int64
	CatchAllOwnerUserIDs []int64
}

// groupRecipientsByTarget collapses multiple inbound recipients that share the
// same resolved target inbox into one outbound forward action.
func groupRecipientsByTarget(recipients []ResolvedRecipient) []groupedRecipients {
	groups := make([]groupedRecipients, 0, len(recipients))
	indexByTarget := make(map[string]int, len(recipients))

	for _, item := range recipients {
		targetKind := strings.TrimSpace(item.TargetKind)
		targetKey := ""
		targetEmail := strings.ToLower(strings.TrimSpace(item.TargetEmail))
		targetTokenPublicID := strings.TrimSpace(item.TargetTokenPublicID)
		switch targetKind {
		case "api_token":
			targetKey = "api_token:" + targetTokenPublicID
		default:
			targetKind = "email"
			targetKey = "email:" + targetEmail
		}
		if targetKey == "email:" || targetKey == "api_token:" {
			continue
		}

		index, exists := indexByTarget[targetKey]
		if !exists {
			index = len(groups)
			indexByTarget[targetKey] = index
			groups = append(groups, groupedRecipients{
				TargetKind:           targetKind,
				TargetEmail:          targetEmail,
				TargetTokenPublicID:  targetTokenPublicID,
				OriginalRecipients:   []string{item.OriginalRecipient},
				OwnerUserIDs:         uniqueOwnerIDs(item),
				CatchAllOwnerUserIDs: uniqueCatchAllOwners(item),
			})
			continue
		}

		groups[index].OriginalRecipients = append(groups[index].OriginalRecipients, item.OriginalRecipient)
		if item.RouteOwnerUserID > 0 && !containsInt64(groups[index].OwnerUserIDs, item.RouteOwnerUserID) {
			groups[index].OwnerUserIDs = append(groups[index].OwnerUserIDs, item.RouteOwnerUserID)
		}
		if item.UsedCatchAll && item.RouteOwnerUserID > 0 && !containsInt64(groups[index].CatchAllOwnerUserIDs, item.RouteOwnerUserID) {
			groups[index].CatchAllOwnerUserIDs = append(groups[index].CatchAllOwnerUserIDs, item.RouteOwnerUserID)
		}
	}

	return groups
}

// uniqueOwnerIDs starts one group with the single owner that should be charged
// against the generic per-user daily forwarding cap.
func uniqueOwnerIDs(item ResolvedRecipient) []int64 {
	if item.RouteOwnerUserID <= 0 {
		return nil
	}
	return []int64{item.RouteOwnerUserID}
}

// uniqueCatchAllOwners starts one group with the single owner that should be
// charged for a catch-all delivery when the route actually used catch-all.
func uniqueCatchAllOwners(item ResolvedRecipient) []int64 {
	if !item.UsedCatchAll || item.RouteOwnerUserID <= 0 {
		return nil
	}
	return []int64{item.RouteOwnerUserID}
}

// containsInt64 reports whether the given owner was already counted for this
// final forward action.
func containsInt64(items []int64, target int64) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
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
	case errors.Is(err, ErrTargetNotVerified), errors.Is(err, ErrTargetOwnershipMismatch), errors.Is(err, ErrTargetTokenUnavailable):
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
			Message:      "remote smtp delivery timed out",
		}
	default:
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "failed to forward message upstream",
		}
	}
}

// queueSMTPError converts durable-queue failures into SMTP DATA errors.
func queueSMTPError(err error) error {
	switch {
	case errors.Is(err, ErrCatchAllAccessUnavailable), errors.Is(err, ErrCatchAllDailyLimitExceeded):
		return catchAllAccessSMTPError(err)
	case errors.Is(err, ErrForwardingDailyLimitExceeded):
		return &smtp.SMTPError{
			Code:         452,
			EnhancedCode: smtp.EnhancedCode{4, 2, 2},
			Message:      "recipient daily forwarding limit has been reached",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 1},
			Message:      "mail delivery queue timed out",
		}
	default:
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "failed to queue message for delivery",
		}
	}
}

// acquireIngressSlot applies backpressure before the session reads the full
// message body into memory so the server cannot accept unlimited concurrent
// DATA uploads under burst load.
func (s *smtpSession) acquireIngressSlot() bool {
	if s == nil || cap(s.ingressSlots) == 0 {
		return true
	}

	timer := time.NewTimer(s.enqueueTimeout)
	defer timer.Stop()

	select {
	case s.ingressSlots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// releaseIngressSlot frees one previously acquired DATA concurrency slot.
func (s *smtpSession) releaseIngressSlot() {
	if s == nil || cap(s.ingressSlots) == 0 {
		return
	}
	select {
	case <-s.ingressSlots:
	default:
	}
}

// catchAllAccessSMTPError converts deterministic catch-all access denials into
// SMTP DATA failures that correctly signal whether the sender should retry.
func catchAllAccessSMTPError(err error) error {
	switch {
	case errors.Is(err, ErrCatchAllAccessUnavailable):
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 2, 1},
			Message:      "recipient catch-all access is unavailable",
		}
	case errors.Is(err, ErrCatchAllDailyLimitExceeded):
		return &smtp.SMTPError{
			Code:         452,
			EnhancedCode: smtp.EnhancedCode{4, 2, 2},
			Message:      "recipient catch-all daily limit has been reached",
		}
	default:
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 4, 0},
			Message:      "temporary error while checking recipient catch-all access",
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
