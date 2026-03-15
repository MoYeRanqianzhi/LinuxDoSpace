package model

import "time"

const (
	// MailDeliveryJobStatusQueued means the message is durably stored and is
	// waiting for one worker to claim it for outbound delivery.
	MailDeliveryJobStatusQueued = "queued"

	// MailDeliveryJobStatusProcessing means one worker already leased the job
	// and is currently attempting to forward it to the final mailbox target.
	MailDeliveryJobStatusProcessing = "processing"

	// MailDeliveryJobStatusDelivered means the remote SMTP side accepted the
	// message and LinuxDoSpace persisted that terminal success state.
	MailDeliveryJobStatusDelivered = "delivered"

	// MailDeliveryJobStatusFailed means the job exhausted all retry attempts and
	// any reserved catch-all quota was refunded atomically.
	MailDeliveryJobStatusFailed = "failed"
)

// MailDeliveryReservation records one catch-all quota reservation that was
// consumed when the inbound SMTP transaction was durably enqueued.
type MailDeliveryReservation struct {
	UserID       int64  `json:"user_id"`
	Count        int64  `json:"count"`
	ConsumedMode string `json:"consumed_mode"`
	UsageDate    string `json:"usage_date"`
}

// MailDeliveryJob stores one durable outbound delivery task together with the
// original message payload and the accounting metadata needed for retries and
// terminal refunds.
type MailDeliveryJob struct {
	ID                   int64                     `json:"id"`
	MessageID            int64                     `json:"message_id"`
	OriginalEnvelopeFrom string                    `json:"original_envelope_from"`
	OriginalRecipients   []string                  `json:"original_recipients"`
	TargetRecipients     []string                  `json:"target_recipients"`
	RawMessage           []byte                    `json:"-"`
	MessageSizeBytes     int64                     `json:"message_size_bytes"`
	Reservations         []MailDeliveryReservation `json:"reservations"`
	Status               string                    `json:"status"`
	AttemptCount         int                       `json:"attempt_count"`
	MaxAttempts          int                       `json:"max_attempts"`
	NextAttemptAt        time.Time                 `json:"next_attempt_at"`
	ProcessingStartedAt  *time.Time                `json:"processing_started_at,omitempty"`
	LastAttemptAt        *time.Time                `json:"last_attempt_at,omitempty"`
	LastError            string                    `json:"last_error"`
	DeliveredAt          *time.Time                `json:"delivered_at,omitempty"`
	FailedAt             *time.Time                `json:"failed_at,omitempty"`
	CreatedAt            time.Time                 `json:"created_at"`
	UpdatedAt            time.Time                 `json:"updated_at"`
}
