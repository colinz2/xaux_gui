package gui

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestDiagnosticErrorPreservesLocalizedMessageAndCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := localizedError("请求失败", "request failed", cause)
	if got := err.Error(); got != "请求失败：connection refused" {
		t.Fatalf("user-facing error = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("localized error did not preserve its cause")
	}
	if got := diagnosticError(err); got != "request failed: connection refused" {
		t.Fatalf("diagnostic error = %q", got)
	}
	wrapped := &url.Error{Op: "Post", URL: "https://example.test", Err: err}
	if got := diagnosticError(wrapped); !strings.Contains(got, `Post "https://example.test": request failed: connection refused`) {
		t.Fatalf("URL error lost its English diagnostic or context: %q", got)
	}
	if diagnosticError(nil) != "<nil>" || diagnosticError(cause) != cause.Error() {
		t.Fatal("nil and external errors must be preserved")
	}
	external := errors.New("外部错误")
	if diagnosticError(external) != external.Error() {
		t.Fatal("external error details must not be discarded or translated")
	}
}
