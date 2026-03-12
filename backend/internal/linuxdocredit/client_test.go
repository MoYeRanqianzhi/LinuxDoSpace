package linuxdocredit

import (
	"net/url"
	"testing"
)

// TestSignValuesMatchesDeterministicPayload locks the MD5 signing helper to a
// stable payload so accidental field-order regressions are caught immediately.
func TestSignValuesMatchesDeterministicPayload(t *testing.T) {
	values := url.Values{}
	values.Set("money", "10")
	values.Set("name", "Test")
	values.Set("out_trade_no", "M20250101")
	values.Set("pid", "001")
	values.Set("type", "epay")

	got := SignValues(values, "secret")
	const want = "640d0472ca9e94ab7c908f37b9a4ffe1"
	if got != want {
		t.Fatalf("expected sign %s, got %s", want, got)
	}
}

// TestVerifyNotification rejects tampered callbacks and accepts correctly
// signed success notifications.
func TestVerifyNotification(t *testing.T) {
	client := NewClient("001", "secret", "https://credit.linux.do/epay", "", "", 0)

	values := url.Values{}
	values.Set("pid", "001")
	values.Set("trade_no", "T20250101")
	values.Set("out_trade_no", "M20250101")
	values.Set("type", "epay")
	values.Set("name", "Test")
	values.Set("money", "10")
	values.Set("trade_status", "TRADE_SUCCESS")
	values.Set("sign_type", "MD5")
	values.Set("sign", SignValues(values, "secret"))

	notification, err := client.VerifyNotification(values)
	if err != nil {
		t.Fatalf("expected signed notification to verify, got %v", err)
	}
	if notification.OutTradeNo != "M20250101" {
		t.Fatalf("expected out_trade_no M20250101, got %s", notification.OutTradeNo)
	}

	values.Set("sign", "bad-signature")
	if _, err := client.VerifyNotification(values); err == nil {
		t.Fatalf("expected invalid signature to be rejected")
	}
}
