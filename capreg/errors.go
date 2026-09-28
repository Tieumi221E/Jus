package capreg

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind of failure, part of the interface: the exit code of the command
// line and the HTTP status of the window's API follow from it.
type ErrKind string

const (
	KindFailed   ErrKind = "failed"   // exit 1, HTTP 422
	KindNotFound ErrKind = "notfound" // exit 1, HTTP 404
	KindUsage    ErrKind = "usage"    // exit 2, HTTP 400
	KindConflict ErrKind = "conflict" // exit 3, HTTP 409: the data changed since the version the call was based on
	KindConfirm  ErrKind = "confirm"  // exit 2, HTTP 400: a Confirm capability without yes
)

// Exit codes of the command line.
const (
	ExitOK       = 0
	ExitFail     = 1
	ExitUsage    = 2
	ExitConflict = 3
)

// Error is a failure of a known kind.
type Error struct {
	Kind ErrKind `json:"kind"`
	Msg  string  `json:"error"`
	// Plan, for KindConfirm, is what the call would do.
	Plan any `json:"plan,omitempty"`
}

func (e *Error) Error() string { return e.Msg }

func Usagef(format string, a ...any) error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, a...)}
}

func NotFoundf(format string, a ...any) error {
	return &Error{Kind: KindNotFound, Msg: fmt.Sprintf(format, a...)}
}

func Conflictf(format string, a ...any) error {
	return &Error{Kind: KindConflict, Msg: fmt.Sprintf(format, a...)}
}

// NeedConfirm is returned by a Confirm capability called without yes; plan
// says what it would do.
func NeedConfirm(msg string, plan any) error {
	return &Error{Kind: KindConfirm, Msg: msg, Plan: plan}
}

// AsError gives err a kind: *Error as it is, anything else failed.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: KindFailed, Msg: err.Error()}
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	switch AsError(err).Kind {
	case KindUsage, KindConfirm:
		return ExitUsage
	case KindConflict:
		return ExitConflict
	}
	return ExitFail
}

func HTTPStatus(err error) int {
	switch AsError(err).Kind {
	case KindUsage, KindConfirm:
		return http.StatusBadRequest
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}

// FromStatus is the kind for an HTTP status from another instance's API.
func FromStatus(code int) ErrKind {
	switch code {
	case http.StatusBadRequest:
		return KindUsage
	case http.StatusNotFound:
		return KindNotFound
	case http.StatusConflict:
		return KindConflict
	}
	return KindFailed
}
