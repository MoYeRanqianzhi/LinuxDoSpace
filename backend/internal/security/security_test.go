package security

import "testing"

// TestNormalizePathOnly 验证登录完成后的跳转路径会被限制在站内相对路径。
func TestNormalizePathOnly(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{input: "/settings", expected: "/settings"},
		{input: "", expected: "/"},
		{input: "https://example.com", expected: "/"},
		{input: "//evil.example.com", expected: "/"},
	}

	for _, testCase := range testCases {
		if actual := NormalizePathOnly(testCase.input); actual != testCase.expected {
			t.Fatalf("input %q: expected %q, got %q", testCase.input, testCase.expected, actual)
		}
	}
}

// TestCodeChallengeS256 验证 PKCE S256 challenge 会输出非空、稳定的结果。
func TestCodeChallengeS256(t *testing.T) {
	first := CodeChallengeS256("verifier-123")
	second := CodeChallengeS256("verifier-123")
	third := CodeChallengeS256("verifier-456")

	if first == "" {
		t.Fatalf("expected non-empty challenge")
	}
	if first != second {
		t.Fatalf("expected deterministic challenge for same verifier")
	}
	if first == third {
		t.Fatalf("expected different verifiers to produce different challenges")
	}
}
