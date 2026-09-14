package wire

import (
	"fmt"
	"net/http"
)

// Code is the stable, machine-readable identifier a client branches on. It is
// snake_case, it never changes once published, and a client that does not
// recognise one must fall back to the HTTP status alone — which is what makes
// adding a code a backwards-compatible change.
type Code string

// The v1 codes, with the client behaviour each one demands (API-V1.md,
// "Errors"). Nothing outside this list may appear in a v1 response.
const (
	// CodeUnauthorized (401): the token is not valid. Discard it and return to
	// the pairing screen.
	CodeUnauthorized Code = "unauthorized"
	// CodeForbidden (403): the feature is not available to this account.
	// Disable the button and tell the driver once.
	CodeForbidden Code = "forbidden"
	// CodeNotFound (404): no such resource.
	CodeNotFound Code = "not_found"
	// CodeConflict (409): an idempotency key was reused with a different body.
	// That is a bug in the caller: log it, never retry it.
	CodeConflict Code = "conflict"
	// CodeInvalid (422): the body failed validation. Log Detail, drop the
	// payload, do not retry.
	CodeInvalid Code = "invalid"
	// CodeRateLimited (429): back off for RetryAfterS and then resume. It
	// applies to the whole client, not to one endpoint.
	CodeRateLimited Code = "rate_limited"
	// CodeServerError (500): retry with backoff up to the client's cap.
	CodeServerError Code = "server_error"
	// CodeClientTooOld (426): the client is older than Discovery.MinClient.
	// Stop uploading and tell the driver to update.
	CodeClientTooOld Code = "client_too_old"
)

// Codes returns the v1 codes in the order API-V1.md tabulates them. The returned
// slice is freshly allocated, so a caller may keep or sort it.
func Codes() []Code {
	return []Code{
		CodeUnauthorized, CodeForbidden, CodeNotFound, CodeConflict,
		CodeInvalid, CodeRateLimited, CodeServerError, CodeClientTooOld,
	}
}

// Known reports whether c is one of the v1 codes. A server must only send known
// codes; a client must treat an unknown one as the HTTP status alone rather than
// as an error in itself.
func (c Code) Known() bool {
	switch c {
	case CodeUnauthorized, CodeForbidden, CodeNotFound, CodeConflict,
		CodeInvalid, CodeRateLimited, CodeServerError, CodeClientTooOld:
		return true
	default:
		return false
	}
}

// HTTPStatus is the status the spec's error table pairs with the code, or zero
// for an unknown code. It exists so that the two sides cannot drift: the server
// chooses its status from here and the client's expectations come from the same
// table.
func (c Code) HTTPStatus() int {
	switch c {
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeInvalid:
		return http.StatusUnprocessableEntity
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeServerError:
		return http.StatusInternalServerError
	case CodeClientTooOld:
		return http.StatusUpgradeRequired
	default:
		return 0
	}
}

// Retryable reports whether a client may retry the request that produced this
// code. Only 429 and 5xx are retryable; every other 4xx is a permanent answer
// and retrying it just burns the driver's connection (API-V1.md, "Rate limits
// and retries").
func (c Code) Retryable() bool {
	return c == CodeRateLimited || c == CodeServerError
}

// ErrorEnvelope is the body of every non-2xx response. The envelope exists so a
// success body can never be mistaken for a failure body by shape alone:
//
//	{ "error": { "code": "conflict", "message": "...", "detail": {...},
//	             "retry_after_s": 0 } }
type ErrorEnvelope struct {
	Error *Error `json:"error"`
}

// Error is the machine-readable-first payload inside an [ErrorEnvelope].
//
// Code is what the client branches on. Message is shown to the driver exactly as
// written, so a server must make it a complete sentence with no jargon, no
// identifier and no stack detail. Detail is free-form context for the client's
// log and is never shown. RetryAfterS is meaningful for CodeRateLimited and is
// zero elsewhere.
//
// It implements error, so a client can return one straight up its own stack and
// recover it again with errors.As.
type Error struct {
	Code        Code           `json:"code"`
	Message     string         `json:"message"`
	Detail      map[string]any `json:"detail,omitempty"`
	RetryAfterS int            `json:"retry_after_s,omitempty"`
}

// Error implements the error interface. The rendering is for a log, never for
// the driver: the driver sees Message.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewError builds the envelope for one code and message. It is the only
// constructor either side needs, and it keeps the envelope from being built by
// hand differently in twenty places.
func NewError(code Code, message string) *ErrorEnvelope {
	return &ErrorEnvelope{Error: &Error{Code: code, Message: message}}
}
