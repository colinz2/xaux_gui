package gui

import (
	"fmt"
	"net/url"
)

type localizedDiagnosticError struct {
	message    string
	messageKey string
	data       any
	diagnostic string
	cause      error
}

func (e *localizedDiagnosticError) Error() string {
	message := e.message
	if e.messageKey != "" {
		message = (&localizedText{key: e.messageKey, data: e.data}).text()
	}
	if e.cause == nil {
		return message
	}
	return trData("common.error_cause", map[string]any{"Message": message, "Cause": e.cause.Error()})
}

func localizedMessageError(key string, data any, diagnostic string, cause error) error {
	return &localizedDiagnosticError{messageKey: key, data: data, diagnostic: diagnostic, cause: cause}
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
