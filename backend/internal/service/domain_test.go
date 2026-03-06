package service

import (
	"testing"

	"linuxdospace/backend/internal/model"
)

// TestNormalizePrefix 验证用户输入前缀会被正确清洗成 DNS label。
func TestNormalizePrefix(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{name: "simple", input: "alice", expected: "alice"},
		{name: "mixed case", input: "Alice-01", expected: "alice-01"},
		{name: "invalid chars", input: "Alice 中文 @@@", expected: "alice"},
		{name: "blank", input: "   ", expectError: true},
		{name: "symbols only", input: "@@@", expectError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := NormalizePrefix(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

// TestNormalizeRelativeRecordName 验证命名空间内相对记录名的校验逻辑。
func TestNormalizeRelativeRecordName(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{name: "root record", input: "@", expected: "@"},
		{name: "wildcard child", input: "*.api", expected: "*.api"},
		{name: "nested child", input: "WWW.Api", expected: "www.api"},
		{name: "invalid label", input: "bad_label", expectError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := NormalizeRelativeRecordName(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

// TestNamespaceHelpers 验证绝对名称与相对名称之间的转换逻辑。
func TestNamespaceHelpers(t *testing.T) {
	namespace := "alice.linuxdo.space"

	fullName := BuildAbsoluteName("@", namespace)
	if fullName != namespace {
		t.Fatalf("expected namespace root, got %q", fullName)
	}

	childName := BuildAbsoluteName("www.api", namespace)
	if childName != "www.api.alice.linuxdo.space" {
		t.Fatalf("unexpected child name: %q", childName)
	}

	if !BelongsToNamespace(childName, namespace) {
		t.Fatalf("expected child name to belong to namespace")
	}

	if BelongsToNamespace("admin.linuxdo.space", namespace) {
		t.Fatalf("unexpected cross-namespace match")
	}

	if actual := RelativeNameFromAbsolute(childName, namespace); actual != "www.api" {
		t.Fatalf("expected relative name \"www.api\", got %q", actual)
	}
}

// TestValidateAndNormalizeRecordPayload 验证记录内容的类型约束。
func TestValidateAndNormalizeRecordPayload(t *testing.T) {
	if _, _, _, err := validateAndNormalizeRecordPayload("A", "1.1.1.1", nil, true); err != nil {
		t.Fatalf("expected valid A record, got error: %v", err)
	}

	if _, _, _, err := validateAndNormalizeRecordPayload("AAAA", "2001:db8::1", nil, false); err != nil {
		t.Fatalf("expected valid AAAA record, got error: %v", err)
	}

	priority := 10
	if _, actualPriority, actualProxied, err := validateAndNormalizeRecordPayload("MX", "mail.example.com", &priority, true); err != nil {
		t.Fatalf("expected valid MX record, got error: %v", err)
	} else if actualPriority == nil || *actualPriority != 10 || actualProxied {
		t.Fatalf("expected MX priority=10 and proxied=false, got priority=%v proxied=%v", actualPriority, actualProxied)
	}

	if _, _, _, err := validateAndNormalizeRecordPayload("A", "not-an-ip", nil, false); err == nil {
		t.Fatalf("expected invalid A record to fail")
	}
}

// TestEnsureTemporaryUsernameMatch 验证当前临时策略只放行“用户名同名”的子域名前缀。
func TestEnsureTemporaryUsernameMatch(t *testing.T) {
	user := model.User{
		Username: "Alice",
	}

	if err := ensureTemporaryUsernameMatch(user, "alice"); err != nil {
		t.Fatalf("expected alice to be allowed, got %v", err)
	}
	if err := ensureTemporaryUsernameMatch(user, "other-name"); err == nil {
		t.Fatalf("expected mismatched prefix to be rejected")
	}
}

// TestEnsureTemporaryRootRecordOnly 验证当前临时策略只允许根记录 `@`。
func TestEnsureTemporaryRootRecordOnly(t *testing.T) {
	if err := ensureTemporaryRootRecordOnly("@"); err != nil {
		t.Fatalf("expected root record to be allowed, got %v", err)
	}
	if err := ensureTemporaryRootRecordOnly("www"); err == nil {
		t.Fatalf("expected child record to be rejected")
	}
}
