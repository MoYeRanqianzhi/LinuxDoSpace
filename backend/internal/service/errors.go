package service

import "fmt"

// Error 是业务层统一使用的错误类型。
// 统一错误结构的目的，是让 HTTP 层能够稳定地映射状态码、错误码和消息文本。
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Cause      error
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

// Unwrap 允许 errors.Is / errors.As 追溯底层原因。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ValidationError 表示请求参数或业务输入不合法。
func ValidationError(message string) *Error {
	return &Error{StatusCode: 400, Code: "validation_failed", Message: message}
}

// UnauthorizedError 表示用户未登录或会话失效。
func UnauthorizedError(message string) *Error {
	return &Error{StatusCode: 401, Code: "unauthorized", Message: message}
}

// ForbiddenError 表示用户没有访问或操作权限。
func ForbiddenError(message string) *Error {
	return &Error{StatusCode: 403, Code: "forbidden", Message: message}
}

// NotFoundError 表示请求资源不存在。
func NotFoundError(message string) *Error {
	return &Error{StatusCode: 404, Code: "not_found", Message: message}
}

// ConflictError 表示资源冲突，例如域名前缀已被占用。
func ConflictError(message string) *Error {
	return &Error{StatusCode: 409, Code: "conflict", Message: message}
}

// TooManyRequestsError indicates that one sensitive endpoint temporarily rejects
// additional requests because the caller exceeded a security threshold.
func TooManyRequestsError(message string) *Error {
	return &Error{StatusCode: 429, Code: "too_many_requests", Message: message}
}

// UnavailableError 表示必要外部依赖未配置或不可用。
func UnavailableError(message string, cause error) *Error {
	return &Error{StatusCode: 503, Code: "service_unavailable", Message: message, Cause: cause}
}

// InternalError 表示无法进一步细分的内部错误。
func InternalError(message string, cause error) *Error {
	return &Error{StatusCode: 500, Code: "internal_error", Message: message, Cause: cause}
}

// NormalizeError 把任意 error 收敛为 *Error，避免 HTTP 层散落条件分支。
func NormalizeError(err error) *Error {
	if err == nil {
		return nil
	}
	if typed, ok := err.(*Error); ok {
		return typed
	}
	return InternalError("internal server error", err)
}
