package mailrelay

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSMTPForwarderDirectMXDelivery verifies that the outbound forwarder
// delivers mail directly to the recipient domain's MX hosts.
func TestSMTPForwarderDirectMXDelivery(t *testing.T) {
	server := newFakeSMTPServer(t)
	defer server.Close()

	forwarder := &SMTPForwarder{
		from:        "relay@mail.linuxdo.space",
		helloDomain: "mail.linuxdo.space",
		requireTLS:  false,
		mxCacheTTL:  time.Hour,
		lookupMX: func(ctx context.Context, name string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
		},
		dialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			if address != "mx.example.test:25" {
				return nil, fmt.Errorf("unexpected direct-mx dial target %q", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Address())
		},
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
		mxCache:       make(map[string]mxCacheEntry),
		domainLimiter: newDomainConcurrencyLimiter(4),
	}

	err := forwarder.Forward(context.Background(), ForwardRequest{
		OriginalEnvelopeFrom: "sender@example.org",
		OriginalEnvelopeTo:   []string{"alias@alice.linuxdo.space"},
		TargetRecipients:     []string{"target@example.com"},
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
	})
	if err != nil {
		t.Fatalf("forward through direct mx delivery: %v", err)
	}

	session := server.WaitForSession(t)
	if session.hello != "EHLO mail.linuxdo.space" {
		t.Fatalf("expected explicit EHLO domain, got %q", session.hello)
	}
	if session.mailFrom != "FROM:<relay@mail.linuxdo.space>" {
		t.Fatalf("expected relay envelope sender, got %q", session.mailFrom)
	}
	if len(session.rcptTo) != 1 || session.rcptTo[0] != "TO:<target@example.com>" {
		t.Fatalf("expected one forwarded recipient, got %+v", session.rcptTo)
	}
	if !strings.Contains(session.message, "X-LinuxDoSpace-Relay: 1") {
		t.Fatalf("expected relay marker header in forwarded message, got %q", session.message)
	}
}

// TestSMTPForwarderCachesMXLookups verifies that repeated delivery to the same
// domain reuses the in-memory MX cache instead of hitting DNS every time.
func TestSMTPForwarderCachesMXLookups(t *testing.T) {
	server := newFakeSMTPServer(t)
	defer server.Close()

	lookupCount := 0
	forwarder := &SMTPForwarder{
		from:            "relay@mail.linuxdo.space",
		helloDomain:     "mail.linuxdo.space",
		requireTLS:      false,
		mxLookupTimeout: time.Second,
		mxCacheTTL:      time.Hour,
		lookupMX: func(ctx context.Context, name string) ([]*net.MX, error) {
			lookupCount++
			return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
		},
		dialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Address())
		},
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
		mxCache:       make(map[string]mxCacheEntry),
		domainLimiter: newDomainConcurrencyLimiter(4),
	}

	send := func() {
		t.Helper()
		err := forwarder.Forward(context.Background(), ForwardRequest{
			OriginalEnvelopeFrom: "sender@example.org",
			OriginalEnvelopeTo:   []string{"alias@alice.linuxdo.space"},
			TargetRecipients:     []string{"target@example.com"},
			RawMessage:           []byte("Subject: test\r\n\r\nbody"),
		})
		if err != nil {
			t.Fatalf("forward message with mx cache: %v", err)
		}
		_ = server.WaitForSession(t)
	}

	send()
	send()

	if lookupCount != 1 {
		t.Fatalf("expected one MX lookup thanks to cache reuse, got %d", lookupCount)
	}
}

// TestSMTPForwarderRejectsPlaintextServerWhenTLSRequired verifies that the
// forwarder fails closed when a remote MX does not advertise STARTTLS and the
// deployment keeps TLS enforcement enabled.
func TestSMTPForwarderRejectsPlaintextServerWhenTLSRequired(t *testing.T) {
	server := newFakeSMTPServer(t)
	defer server.Close()

	forwarder := &SMTPForwarder{
		from:        "relay@mail.linuxdo.space",
		helloDomain: "mail.linuxdo.space",
		requireTLS:  true,
		mxCacheTTL:  time.Hour,
		lookupMX: func(ctx context.Context, name string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.example.test.", Pref: 10}}, nil
		},
		dialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Address())
		},
		now: func() time.Time {
			return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		},
		mxCache:       make(map[string]mxCacheEntry),
		domainLimiter: newDomainConcurrencyLimiter(4),
	}

	err := forwarder.Forward(context.Background(), ForwardRequest{
		OriginalEnvelopeFrom: "sender@example.org",
		OriginalEnvelopeTo:   []string{"alias@alice.linuxdo.space"},
		TargetRecipients:     []string{"target@example.com"},
		RawMessage:           []byte("Subject: test\r\n\r\nbody"),
	})
	if err == nil {
		t.Fatalf("expected plaintext smtp server to be rejected when requireTLS=true")
	}
	if !strings.Contains(err.Error(), "does not advertise STARTTLS") {
		t.Fatalf("expected STARTTLS rejection, got %v", err)
	}
}

// TestDomainConcurrencyLimiterBlocksUntilRelease verifies that the per-domain
// limiter really enforces its cap instead of allowing unlimited same-domain
// bursts.
func TestDomainConcurrencyLimiterBlocksUntilRelease(t *testing.T) {
	limiter := newDomainConcurrencyLimiter(1)
	domain := "example.com"

	if err := limiter.acquire(context.Background(), domain); err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- limiter.acquire(ctx, domain)
	}()

	select {
	case err := <-waitCh:
		t.Fatalf("expected second acquire to block until timeout, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	limiter.release(domain)
	if err := <-waitCh; err != nil {
		t.Fatalf("expected second acquire to proceed after release, got %v", err)
	}
}

// fakeSMTPSession stores the SMTP conversation captured by the test server.
type fakeSMTPSession struct {
	hello    string
	mailFrom string
	rcptTo   []string
	message  string
}

// fakeSMTPServer implements just enough of the SMTP protocol for the forwarder
// tests to assert EHLO, MAIL FROM, RCPT TO, DATA, and QUIT behavior.
type fakeSMTPServer struct {
	t        *testing.T
	listener net.Listener

	sessionCh chan fakeSMTPSession
	closeOnce sync.Once
}

// newFakeSMTPServer starts the lightweight SMTP listener used by the tests.
func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake smtp server: %v", err)
	}

	server := &fakeSMTPServer{
		t:         t,
		listener:  listener,
		sessionCh: make(chan fakeSMTPSession, 2),
	}
	go server.serve()
	return server
}

// Address returns the TCP address accepted by the fake SMTP listener.
func (s *fakeSMTPServer) Address() string {
	return s.listener.Addr().String()
}

// Close stops the fake listener at the end of the test.
func (s *fakeSMTPServer) Close() {
	s.closeOnce.Do(func() {
		_ = s.listener.Close()
	})
}

// WaitForSession blocks until one SMTP delivery completed or the test timed out.
func (s *fakeSMTPServer) WaitForSession(t *testing.T) fakeSMTPSession {
	t.Helper()

	select {
	case session := <-s.sessionCh:
		return session
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for fake smtp session")
		return fakeSMTPSession{}
	}
}

// serve accepts one test connection at a time and records the SMTP commands it
// receives from the forwarder under test.
func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConnection(conn)
	}
}

// handleConnection speaks the minimum SMTP protocol surface needed by
// net/smtp.Client so the tests can stay hermetic and deterministic.
func (s *fakeSMTPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeLine := func(line string) {
		if _, err := writer.WriteString(line + "\r\n"); err == nil {
			_ = writer.Flush()
		}
	}

	writeLine("220 fake-smtp.local ESMTP ready")

	session := fakeSMTPSession{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(command, "EHLO "), strings.HasPrefix(command, "HELO "):
			session.hello = command
			writeLine("250-fake-smtp.local")
			writeLine("250 PIPELINING")
		case strings.HasPrefix(command, "MAIL "):
			session.mailFrom = strings.TrimSpace(strings.TrimPrefix(command, "MAIL"))
			writeLine("250 2.1.0 OK")
		case strings.HasPrefix(command, "RCPT "):
			session.rcptTo = append(session.rcptTo, strings.TrimSpace(strings.TrimPrefix(command, "RCPT")))
			writeLine("250 2.1.5 OK")
		case command == "DATA":
			writeLine("354 End data with <CR><LF>.<CR><LF>")
			data, dataErr := readSMTPData(reader)
			if dataErr != nil {
				return
			}
			session.message = data
			writeLine("250 2.0.0 queued")
		case command == "QUIT":
			writeLine("221 2.0.0 bye")
			s.sessionCh <- session
			return
		default:
			writeLine("250 OK")
		}
	}
}

// readSMTPData consumes one SMTP DATA block until the terminating dot line.
func readSMTPData(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if line == ".\r\n" {
			return builder.String(), nil
		}
		builder.WriteString(line)
	}
}
