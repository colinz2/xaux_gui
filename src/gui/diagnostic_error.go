package gui

import (
	"fmt"
	"net/url"
)

type localizedDiagnosticError struct {
	message    string
	diagnostic string
	cause      error
}

func (e *localizedDiagnosticError) Error() string {
	if e.cause == nil {
		return e.message
	}
	return e.message + "：" + e.cause.Error()
}

func (e *localizedDiagnosticError) Unwrap() error {
	return e.cause
}

func localizedError(message, diagnostic string, cause error) error {
	return &localizedDiagnosticError{message: message, diagnostic: diagnostic, cause: cause}
}

func diagnosticError(err error) string {
	switch e := err.(type) {
	case nil:
		return "<nil>"
	case *localizedDiagnosticError:
		if e.cause == nil {
			return e.diagnostic
		}
		return e.diagnostic + ": " + diagnosticError(e.cause)
	case *url.Error:
		return fmt.Sprintf("%s %q: %s", e.Op, e.URL, diagnosticError(e.Err))
	default:
		return err.Error()
	}
}
