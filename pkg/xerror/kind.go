package xerror

import "strings"

//go:generate -command enumer go tool enumer
//go:generate enumer -type=Kind -transform=snake -output=kind_string.go -text -linecomment

// Kind classifies an error into a domain-agnostic category.
type Kind int

const (
	// KindNotFound indicates the requested resource does not exist.
	KindNotFound Kind = iota + 1 // not_found
	// KindAlreadyExists indicates a uniqueness conflict.
	KindAlreadyExists // already_exists
	// KindInvalidInput indicates the caller supplied invalid data.
	KindInvalidInput // invalid_input
	// KindUnauthorized indicates an authentication failure.
	KindUnauthorized // unauthorized
	// KindConnectionFailed indicates a connection to an external system failed.
	KindConnectionFailed // connection_failed
	// KindTimeout indicates an operation exceeded its deadline.
	KindTimeout // timeout
	// KindUnavailable indicates a required dependency is not reachable.
	KindUnavailable // unavailable
	// KindUnsupported indicates the operation is not supported.
	KindUnsupported // unsupported
	// KindForbidden indicates the caller lacks permission.
	KindForbidden // forbidden
	// KindInternal indicates an unexpected internal failure.
	KindInternal // internal
)

// HTTPStatus returns the HTTP status code for the kind.
func (k Kind) HTTPStatus() int {
	switch k {
	case KindNotFound:
		return 404
	case KindAlreadyExists:
		return 409
	case KindInvalidInput:
		return 400
	case KindUnauthorized:
		return 401
	case KindConnectionFailed:
		return 422
	case KindTimeout:
		return 408
	case KindUnavailable:
		return 422
	case KindUnsupported:
		return 422
	case KindForbidden:
		return 403
	case KindInternal:
		return 500
	default:
		return 500
	}
}

// codeSuffix returns the upper-case suffix used when building an API code.
func (k Kind) codeSuffix() string {
	return strings.ToUpper(k.String())
}

// messageSuffix returns the lower-case suffix used when building a message.
func (k Kind) messageSuffix() string {
	return strings.ReplaceAll(k.String(), "_", " ")
}
