package mailrelay

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
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
// database-backed recipient resolver, the catch-all access manager, and the
// outbound SMTP forwarder.
func NewServer(mail config.MailConfig, resolver RecipientResolver, accessManager CatchAllAccessManager, forwarder MessageForwarder, logger smtp.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}

	backend := &smtpBackend{
		resolver:       resolver,
		accessManager:  accessManager,
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
	accessManager  CatchAllAccessManager
	forwarder      MessageForwarder
	logger         smtp.Logger
	forwardTimeout time.Duration
}

// NewSession allocates the per-connection SMTP session state.
func (b *smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{
		resolver:       b.resolver,
		accessManager:  b.accessManager,
		forwarder:      b.forwarder,
		logger:         b.logger,
		forwardTimeout: b.forwardTimeout,
		recipients:     make([]ResolvedRecipient, 0, 4),
	}, nil
}

// smtpSession stores the current envelope state for one SMTP transaction.
type smtpSession struct {
	resolver       RecipientResolver
	accessManager  CatchAllAccessManager
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

	plans := make([]forwardGroupPlan, 0, len(s.recipients))
	for _, group := range groupRecipientsByTarget(s.recipients) {
		reservations, reservationErr := s.reserveCatchAllUsageForGroup(group)
		if reservationErr != nil {
			for _, prepared := range plans {
				s.releaseCatchAllReservations(prepared.Reservations)
			}
			return catchAllAccessSMTPError(reservationErr)
		}
		plans = append(plans, forwardGroupPlan{
			Group:        group,
			Reservations: reservations,
		})
	}

	return s.forwardGroupsConcurrently(rawMessage, plans)
}

// forwardGroupPlan stores one final target inbox together with the already
// reserved catch-all quota that must be released if this forward action fails.
type forwardGroupPlan struct {
	Group        groupedRecipients
	Reservations []CatchAllUsageReservation
}

// forwardGroupResult captures the completion status of one concurrent forward
// action so the SMTP session can surface the first failure after fan-out ends.
type forwardGroupResult struct {
	Group groupedRecipients
	Err   error
}

// forwardGroupsConcurrently removes the per-target serial bottleneck inside one
// SMTP DATA transaction. go-smtp already handles connection concurrency; this
// helper makes one accepted message fan out to all target inboxes in parallel.
func (s *smtpSession) forwardGroupsConcurrently(rawMessage []byte, plans []forwardGroupPlan) error {
	if len(plans) == 0 {
		return nil
	}

	results := make(chan forwardGroupResult, len(plans))
	forwardBaseCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	var firstFailure sync.Once
	var waitGroup sync.WaitGroup
	for _, plan := range plans {
		waitGroup.Add(1)
		go func(plan forwardGroupPlan) {
			defer waitGroup.Done()

			forwardCtx, cancel := context.WithTimeout(forwardBaseCtx, s.forwardTimeout)
			defer cancel()

			err := s.forwarder.Forward(forwardCtx, ForwardRequest{
				OriginalEnvelopeFrom: s.mailFrom,
				OriginalEnvelopeTo:   plan.Group.OriginalRecipients,
				TargetRecipients:     []string{plan.Group.TargetEmail},
				RawMessage:           rawMessage,
			})
			if err != nil {
				s.releaseCatchAllReservations(plan.Reservations)
				firstFailure.Do(cancelAll)
			}
			results <- forwardGroupResult{
				Group: plan.Group,
				Err:   err,
			}
		}(plan)
	}

	waitGroup.Wait()
	close(results)

	var firstErr error
	for result := range results {
		if result.Err == nil {
			continue
		}
		s.logger.Printf(
			"linuxdospace mail relay forward failed: target=%s recipients=%v err=%v",
			result.Group.TargetEmail,
			result.Group.OriginalRecipients,
			result.Err,
		)
		if firstErr == nil {
			firstErr = result.Err
		}
	}
	if firstErr != nil {
		return forwardSMTPError(firstErr)
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
	TargetEmail          string
	OriginalRecipients   []string
	CatchAllOwnerUserIDs []int64
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
				TargetEmail:          targetEmail,
				OriginalRecipients:   []string{item.OriginalRecipient},
				CatchAllOwnerUserIDs: uniqueCatchAllOwners(item),
			})
			continue
		}

		groups[index].OriginalRecipients = append(groups[index].OriginalRecipients, item.OriginalRecipient)
		if item.UsedCatchAll && item.RouteOwnerUserID > 0 && !containsInt64(groups[index].CatchAllOwnerUserIDs, item.RouteOwnerUserID) {
			groups[index].CatchAllOwnerUserIDs = append(groups[index].CatchAllOwnerUserIDs, item.RouteOwnerUserID)
		}
	}

	return groups
}

// reserveCatchAllUsageForGroup reserves catch-all quota exactly once per owner
// and final forward action for the current target group.
func (s *smtpSession) reserveCatchAllUsageForGroup(group groupedRecipients) ([]CatchAllUsageReservation, error) {
	if s.accessManager == nil || len(group.CatchAllOwnerUserIDs) == 0 {
		return nil, nil
	}

	reservations := make([]CatchAllUsageReservation, 0, len(group.CatchAllOwnerUserIDs))
	for _, ownerUserID := range group.CatchAllOwnerUserIDs {
		reservation, err := s.accessManager.Reserve(context.Background(), ownerUserID, 1)
		if err != nil {
			s.releaseCatchAllReservations(reservations)
			return nil, err
		}
		reservations = append(reservations, reservation)
	}
	return reservations, nil
}

// releaseCatchAllReservations best-effort rolls back reservations that were
// created for a group whose upstream SMTP forward failed.
func (s *smtpSession) releaseCatchAllReservations(reservations []CatchAllUsageReservation) {
	if s.accessManager == nil {
		return
	}
	for _, reservation := range reservations {
		if err := s.accessManager.Release(context.Background(), reservation); err != nil {
			s.logger.Printf(
				"linuxdospace mail relay failed to release catch-all reservation: user_id=%d usage_date=%s err=%v",
				reservation.UserID,
				reservation.UsageDate,
				err,
			)
		}
	}
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
