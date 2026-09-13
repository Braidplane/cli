// Package xerror provides a unified error type for the application.
//
// Sentinel errors are constructed via template functions that pair a Kind
// (category) with an Entity (the affected domain object). The API error code
// and human-readable message are derived automatically from the Kind and
// Entity, following a predictable {ENTITY}_{KIND_SUFFIX} pattern.
//
// Entity-based templates:
//
//	xerror.NotFound(xerror.User)            // USER_NOT_FOUND, "user not found"
//	xerror.AlreadyExists(xerror.UserEmail)  // USER_EMAIL_ALREADY_EXISTS
//	xerror.AlreadyExists(xerror.User)       // USER_ALREADY_EXISTS
//
// Standalone templates for cross-cutting errors:
//
//	xerror.ValidationFailed()   // VALIDATION_FAILED, "validation failed"
//	xerror.InvalidCredentials() // INVALID_CREDENTIALS, "invalid credentials"
package xerror

import (
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

// Error is a structured error type that carries a kind, a stable API code, a
// human-readable message, and an optional wrapped cause.
type Error struct {
	kind    Kind
	code    string
	message string
	err     error
	// publicDetail is optional sanitized client-facing detail that may be exposed
	// in transport responses when explicitly set by the caller.
	publicDetail string
}

// deriveCode builds the API code: {ENTITY}_{KIND_SUFFIX}.
func deriveCode(entity Entity, kind Kind) string {
	return entity.code + "_" + kind.codeSuffix()
}

// deriveMessage builds the message: "{entity} {kind suffix}".
func deriveMessage(entity Entity, kind Kind) string {
	return entity.label + " " + kind.messageSuffix()
}

// newFromEntity creates an Error with code and message derived from entity + kind.
func newFromEntity(kind Kind, entity Entity) *Error {
	return &Error{
		kind:    kind,
		code:    deriveCode(entity, kind),
		message: deriveMessage(entity, kind),
	}
}

// newStandalone creates an Error with an explicit code and message (no entity).
func newStandalone(kind Kind, code, message string) *Error {
	return &Error{kind: kind, code: code, message: message}
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// Kind returns the error's kind.
func (e *Error) Kind() Kind {
	return e.kind
}

// Code returns the error's stable API code.
func (e *Error) Code() string {
	return e.code
}

// Message returns the error's human-readable message.
func (e *Error) Message() string {
	return e.message
}

// PublicDetail returns optional sanitized client-facing detail.
func (e *Error) PublicDetail() string {
	if e == nil {
		return ""
	}

	return e.publicDetail
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.err != nil {
		detail := e.err.Error()
		if e.kind == KindInternal {
			return detail
		}
		if detail == e.message || strings.HasPrefix(detail, e.message+":") {
			return detail
		}

		return fmt.Sprintf("%s: %s", e.message, detail)
	}
	return e.message
}

// Unwrap returns the wrapped cause, if any.
func (e *Error) Unwrap() error {
	return e.err
}

// With returns a copy of the error with the provided cause attached.
func (e *Error) With(err error) *Error {
	if e == nil {
		return nil
	}

	clone := *e
	clone.err = err

	return &clone
}

// WithPublicDetail returns a copy of the error with sanitized client-facing detail attached.
func (e *Error) WithPublicDetail(detail string) *Error {
	if e == nil {
		return nil
	}

	clone := *e
	clone.publicDetail = strings.TrimSpace(detail)

	return &clone
}

// Detail returns a copy of the error with a plain-string detail message attached.
// If the error already has a cause, the new message is prepended and the
// existing cause is preserved in the chain via wrapping.
func (e *Error) Detail(msg string) *Error {
	if e.err != nil {
		return e.With(fmt.Errorf("%s: %w", msg, e.err))
	}
	return e.With(errors.New(msg))
}

// Detailf returns a copy of the error with a formatted detail message attached.
// The caller controls the full error chain via format verbs (e.g. %w to wrap
// an upstream error). Unlike Detail, Detailf does not implicitly preserve any
// pre-existing cause — use %w in the format string when wrapping is needed.
func (e *Error) Detailf(format string, args ...any) *Error {
	return e.With(fmt.Errorf(format, args...))
}

// New creates an Error with an explicit kind, code, and message. Use this in
// domain packages to define local sentinels without adding them to templates.go.
func New(kind Kind, code, message string) *Error {
	return newStandalone(kind, code, message)
}

// Wrapf wraps an error with additional context using fmt-style formatting.
//
// Use Wrapf only when the wrapped error chain already contains an xerror.Error
// and you only need to add context. For third-party/dependency errors, create a
// typed xerror sentinel and attach the cause via .With(err).
func Wrapf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// Is supports errors.Is by matching on kind and code when comparing two
// xerror.Error values. This allows sentinel errors (which have no wrapped
// cause) to match wrapped instances of the same logical error.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.kind == t.kind && e.code == t.code
}

func Is(err, target error) bool {
	return errors.Is(err, target)
}

func As(err error, target any) bool {
	return errors.As(err, target)
}

func Join(errs ...error) error {
	return errors.Join(errs...)
}

func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// PublicDetail finds the first explicit sanitized client-facing detail in an error chain.
func PublicDetail(err error) string {
	return publicDetail(err)
}

func publicDetail(err error) string {
	if err == nil {
		return ""
	}

	if xe, ok := err.(interface{ PublicDetail() string }); ok {
		if detail := xe.PublicDetail(); detail != "" {
			return detail
		}
	}

	type multiUnwrapper interface {
		Unwrap() []error
	}

	if multi, ok := err.(multiUnwrapper); ok {
		for _, child := range multi.Unwrap() {
			if detail := publicDetail(child); detail != "" {
				return detail
			}
		}
	}

	return publicDetail(errors.Unwrap(err))
}
