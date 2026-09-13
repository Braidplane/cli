package xerror_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/xerror"
)

// ---------------------------------------------------------------------------
// Entity-based template constructors
// ---------------------------------------------------------------------------

func TestNotFound(t *testing.T) {
	t.Parallel()

	err := xerror.NotFound(xerror.User)

	assert.Equal(t, xerror.KindNotFound, err.Kind())
	assert.Equal(t, "USER_NOT_FOUND", err.Code())
	assert.Equal(t, "user not found", err.Message())
	assert.Equal(t, "user not found", err.Error())
	assert.NoError(t, err.Unwrap())
}

func TestAlreadyExists(t *testing.T) {
	t.Parallel()

	err := xerror.AlreadyExists(xerror.Member)

	assert.Equal(t, xerror.KindAlreadyExists, err.Kind())
	assert.Equal(t, "MEMBER_ALREADY_EXISTS", err.Code())
	assert.Equal(t, "member already exists", err.Message())
}

func TestAlreadyExistsWithField(t *testing.T) {
	t.Parallel()

	err := xerror.AlreadyExists(xerror.UserEmail)

	assert.Equal(t, xerror.KindAlreadyExists, err.Kind())
	assert.Equal(t, "USER_EMAIL_ALREADY_EXISTS", err.Code())
	assert.Equal(t, "user email already exists", err.Message())
}

func TestAlreadyExistsWithEntity(t *testing.T) {
	t.Parallel()

	err := xerror.AlreadyExists(xerror.DataSource)

	assert.Equal(t, "DATA_SOURCE_ALREADY_EXISTS", err.Code())
	assert.Equal(t, "data source already exists", err.Message())
}

func TestInvalidInput(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidInput(xerror.Organization)

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "ORGANIZATION_INVALID_INPUT", err.Code())
	assert.Equal(t, "organization invalid input", err.Message())
}

func TestRequired(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidRequired(xerror.OrganizationSlug)

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "ORGANIZATION_SLUG_REQUIRED", err.Code())
	assert.Equal(t, "org slug is required", err.Message())
	assert.Equal(t, "org slug is required", err.Error())
}

func TestInternalRequired(t *testing.T) {
	t.Parallel()

	err := xerror.UnexpectedRequired(xerror.QueryExecutor)

	assert.Equal(t, xerror.KindInternal, err.Kind())
	assert.Equal(t, "QUERY_EXECUTOR_REQUIRED", err.Code())
	assert.Equal(t, "query executor is required", err.Message())
	assert.Equal(t, "query executor is required", err.Error())
}

func TestConnectionFailed(t *testing.T) {
	t.Parallel()

	err := xerror.ConnectionFailed(xerror.DataSource)

	assert.Equal(t, xerror.KindConnectionFailed, err.Kind())
	assert.Equal(t, "DATA_SOURCE_CONNECTION_FAILED", err.Code())
	assert.Equal(t, "data source connection failed", err.Message())
	assert.Equal(t, 422, err.Kind().HTTPStatus())
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	err := xerror.Timeout(xerror.Query)

	assert.Equal(t, xerror.KindTimeout, err.Kind())
	assert.Equal(t, "QUERY_TIMEOUT", err.Code())
	assert.Equal(t, "query timeout", err.Message())
	assert.Equal(t, 408, err.Kind().HTTPStatus())
}

func TestExecutionSafetyLimitErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     *xerror.Error
		kind    xerror.Kind
		code    string
		message string
	}{
		{
			name:    "row limit exceeded",
			err:     xerror.ExecutionRowLimitExceeded(),
			kind:    xerror.KindInvalidInput,
			code:    "EXECUTION_ROW_LIMIT_EXCEEDED",
			message: "execution row limit exceeded",
		},
		{
			name:    "response size exceeded",
			err:     xerror.ExecutionResponseSizeExceeded(),
			kind:    xerror.KindInvalidInput,
			code:    "EXECUTION_RESPONSE_SIZE_EXCEEDED",
			message: "execution response size exceeded",
		},
		{
			name:    "request size exceeded",
			err:     xerror.ExecutionRequestSizeExceeded(),
			kind:    xerror.KindInvalidInput,
			code:    "EXECUTION_REQUEST_SIZE_EXCEEDED",
			message: "execution request size exceeded",
		},
		{
			name:    "redirect limit exceeded",
			err:     xerror.ExecutionRedirectLimitExceeded(),
			kind:    xerror.KindInvalidInput,
			code:    "EXECUTION_REDIRECT_LIMIT_EXCEEDED",
			message: "execution redirect limit exceeded",
		},
		{
			name:    "concurrency limit exceeded",
			err:     xerror.ExecutionConcurrencyLimitExceeded(),
			kind:    xerror.KindUnavailable,
			code:    "EXECUTION_CONCURRENCY_LIMIT_EXCEEDED",
			message: "execution concurrency limit exceeded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.kind, tc.err.Kind())
			assert.Equal(t, tc.code, tc.err.Code())
			assert.Equal(t, tc.message, tc.err.Message())
			require.ErrorIs(t, tc.err.Detail("limit detail"), tc.err)
		})
	}
}

func TestUnavailable(t *testing.T) {
	t.Parallel()

	err := xerror.Unavailable(xerror.DataSource)

	assert.Equal(t, xerror.KindUnavailable, err.Kind())
	assert.Equal(t, "DATA_SOURCE_UNAVAILABLE", err.Code())
	assert.Equal(t, "data source unavailable", err.Message())
}

func TestUnsupported(t *testing.T) {
	t.Parallel()

	err := xerror.Unsupported(xerror.DataSource)

	assert.Equal(t, xerror.KindUnsupported, err.Kind())
	assert.Equal(t, "DATA_SOURCE_UNSUPPORTED", err.Code())
	assert.Equal(t, "data source unsupported", err.Message())
}

func TestDataSourceSchemaIntrospectionUnsupported(t *testing.T) {
	t.Parallel()

	err := xerror.DataSourceSchemaIntrospectionUnsupported()

	assert.Equal(t, xerror.KindUnsupported, err.Kind())
	assert.Equal(t, "DATA_SOURCE_SCHEMA_INTROSPECTION_UNSUPPORTED", err.Code())
	assert.Equal(t, "data source schema introspection unsupported", err.Message())
}

func TestInternal(t *testing.T) {
	t.Parallel()

	err := xerror.Unexpected()

	assert.Equal(t, xerror.KindInternal, err.Kind())
	assert.Equal(t, "INTERNAL_ERROR", err.Code())
	assert.Equal(t, "internal error", err.Message())
}

func TestWrapf(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("connection refused")
	err := xerror.Wrapf("dial database: %w", cause)

	require.Error(t, err)
	assert.Equal(t, "dial database: connection refused", err.Error())
	require.ErrorIs(t, err, cause)
}

func TestDetail(t *testing.T) {
	t.Parallel()

	err := xerror.Unexpected().Detail("something went wrong")

	require.NotNil(t, err)
	assert.Equal(t, xerror.KindInternal, err.Kind())
	assert.Equal(t, "INTERNAL_ERROR", err.Code())
	assert.Equal(t, "something went wrong", err.Error())
}

func TestDetailf(t *testing.T) {
	t.Parallel()

	err := xerror.ValidationFailed().Detailf("field %q is required", "name")

	require.NotNil(t, err)
	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "VALIDATION_FAILED", err.Code())
	assert.Equal(t, `validation failed: field "name" is required`, err.Error())
}

func TestWithPublicDetail(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidInput(xerror.DataSource).WithPublicDetail("invalid credentials format")

	require.NotNil(t, err)
	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "DATA_SOURCE_INVALID_INPUT", err.Code())
	assert.Equal(t, "data source invalid input", err.Message())
	assert.Equal(t, "invalid credentials format", err.PublicDetail())
	assert.Equal(t, "data source invalid input", err.Error())
}

func TestTransformSpecRequired(t *testing.T) {
	t.Parallel()

	err := xerror.UnexpectedTransformSpecRequired()

	assert.Equal(t, xerror.KindInternal, err.Kind())
	assert.Equal(t, "PIPELINE_TRANSFORM_SPEC_REQUIRED", err.Code())
	assert.Equal(t, "transform spec is required", err.Message())
}

// ---------------------------------------------------------------------------
// Standalone template constructors
// ---------------------------------------------------------------------------

func TestValidationFailed(t *testing.T) {
	t.Parallel()

	err := xerror.ValidationFailed()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "VALIDATION_FAILED", err.Code())
	assert.Equal(t, "validation failed", err.Message())
}

func TestInvalidCredentials(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidCredentials()

	assert.Equal(t, xerror.KindUnauthorized, err.Kind())
	assert.Equal(t, "INVALID_CREDENTIALS", err.Code())
	assert.Equal(t, "invalid credentials", err.Message())
}

func TestAlreadyVerifiedStandalone(t *testing.T) {
	t.Parallel()

	err := xerror.AlreadyVerified()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "ALREADY_VERIFIED", err.Code())
	assert.Equal(t, "already verified", err.Message())
}

func TestInvalidTokenStandalone(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidToken()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "INVALID_TOKEN", err.Code())
	assert.Equal(t, "invalid token", err.Message())
}

func TestCannotRemoveOwner(t *testing.T) {
	t.Parallel()

	err := xerror.CannotRemoveOwner()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "CANNOT_REMOVE_OWNER", err.Code())
	assert.Equal(t, "cannot remove owner", err.Message())
}

func TestCannotDemoteOwner(t *testing.T) {
	t.Parallel()

	err := xerror.CannotDemoteOwner()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "CANNOT_DEMOTE_OWNER", err.Code())
	assert.Equal(t, "cannot demote owner", err.Message())
}

func TestCannotDeleteDefault(t *testing.T) {
	t.Parallel()

	err := xerror.CannotDeleteDefault(xerror.Role)

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "ROLE_CANNOT_DELETE_DEFAULT", err.Code())
	assert.Equal(t, "cannot delete default role", err.Message())
}

func TestCannotUpdateDefault(t *testing.T) {
	t.Parallel()

	err := xerror.CannotUpdateDefault(xerror.Role)

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "ROLE_CANNOT_UPDATE_DEFAULT", err.Code())
	assert.Equal(t, "cannot update default role", err.Message())
}

func TestParameterValidationFailed(t *testing.T) {
	t.Parallel()

	err := xerror.ParameterValidationFailed()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "PARAMETER_VALIDATION_FAILED", err.Code())
	assert.Equal(t, "parameter validation failed", err.Message())
}

func TestInvalidSchemaURL(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidSchemaURL()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "INVALID_SCHEMA_URL", err.Code())
	assert.Equal(t, "invalid schema url", err.Message())
}

func TestSchemaFetchFailed(t *testing.T) {
	t.Parallel()

	err := xerror.SchemaFetchFailed()

	assert.Equal(t, xerror.KindUnavailable, err.Kind())
	assert.Equal(t, "SCHEMA_FETCH_FAILED", err.Code())
	assert.Equal(t, "schema fetch failed", err.Message())
}

func TestInvalidOpenAPIStandalone(t *testing.T) {
	t.Parallel()

	err := xerror.InvalidOpenAPI()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "INVALID_OPENAPI", err.Code())
	assert.Equal(t, "invalid openapi", err.Message())
}

func TestBuilderStateInvalid(t *testing.T) {
	t.Parallel()

	err := xerror.BuilderStateInvalid()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "BUILDER_STATE_INVALID", err.Code())
	assert.Equal(t, "builder state is invalid", err.Message())
}

func TestSinkUpstreamRequired(t *testing.T) {
	t.Parallel()

	err := xerror.SinkUpstreamRequired()

	assert.Equal(t, xerror.KindInvalidInput, err.Kind())
	assert.Equal(t, "PIPELINE_SINK_UPSTREAM_REQUIRED", err.Code())
	assert.Equal(t, "sink step requires an upstream result", err.Message())
}

func TestUnsupportedURIScheme(t *testing.T) {
	t.Parallel()

	err := xerror.UnsupportedURIScheme()

	assert.Equal(t, xerror.KindUnsupported, err.Kind())
	assert.Equal(t, "UNSUPPORTED_URI_SCHEME", err.Code())
	assert.Equal(t, "unsupported URI scheme", err.Message())
}

// ---------------------------------------------------------------------------
// errors.Is matching
// ---------------------------------------------------------------------------

func TestErrorIs_SameSentinel(t *testing.T) {
	t.Parallel()

	sentinel := xerror.NotFound(xerror.User)
	wrapped := fmt.Errorf("wrapped: %w", sentinel)
	require.ErrorIs(t, wrapped, sentinel)
}

func TestErrorIs_TemplateMatchesTemplate(t *testing.T) {
	t.Parallel()

	a := xerror.NotFound(xerror.User)
	b := xerror.NotFound(xerror.User)

	require.ErrorIs(t, a, b)
}

func TestErrorIs_DifferentEntityDoesNotMatch(t *testing.T) {
	t.Parallel()

	a := xerror.NotFound(xerror.User)
	b := xerror.NotFound(xerror.Role)

	assert.NotErrorIs(t, a, b)
}

func TestErrorIs_DifferentKindDoesNotMatch(t *testing.T) {
	t.Parallel()

	a := xerror.NotFound(xerror.User)
	b := xerror.InvalidInput(xerror.User)

	assert.NotErrorIs(t, a, b)
}

func TestErrorIs_FmtErrorfWrapped(t *testing.T) {
	t.Parallel()

	sentinel := xerror.NotFound(xerror.User)
	wrapped := fmt.Errorf("service layer: %w", sentinel)

	require.ErrorIs(t, wrapped, sentinel)
}

// ---------------------------------------------------------------------------
// errors.As
// ---------------------------------------------------------------------------

func TestErrorAs(t *testing.T) {
	t.Parallel()

	original := xerror.AlreadyExists(xerror.Organization)
	wrapped := fmt.Errorf("svc: %w", original)

	var xe *xerror.Error
	require.ErrorAs(t, wrapped, &xe)
	assert.Equal(t, xerror.KindAlreadyExists, xe.Kind())
	assert.Equal(t, "ORGANIZATION_ALREADY_EXISTS", xe.Code())
	assert.Equal(t, "organization already exists", xe.Message())
}

func TestErrorWith(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("boom")
	err := xerror.InvalidInput(xerror.DataSource).With(cause)

	require.ErrorIs(t, err, xerror.InvalidInput(xerror.DataSource))
	require.ErrorIs(t, err, cause)
	assert.Equal(t, "data source invalid input: boom", err.Error())
}

// ---------------------------------------------------------------------------
// Kind methods
// ---------------------------------------------------------------------------

func TestKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind xerror.Kind
		want string
	}{
		{xerror.KindNotFound, "not_found"},
		{xerror.KindAlreadyExists, "already_exists"},
		{xerror.KindInvalidInput, "invalid_input"},
		{xerror.KindUnauthorized, "unauthorized"},
		{xerror.KindConnectionFailed, "connection_failed"},
		{xerror.KindTimeout, "timeout"},
		{xerror.KindUnavailable, "unavailable"},
		{xerror.KindUnsupported, "unsupported"},
		{xerror.KindForbidden, "forbidden"},
		{xerror.KindInternal, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.kind.String())
		})
	}
}

func TestKind_HTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind   xerror.Kind
		status int
	}{
		{xerror.KindNotFound, 404},
		{xerror.KindAlreadyExists, 409},
		{xerror.KindInvalidInput, 400},
		{xerror.KindUnauthorized, 401},
		{xerror.KindConnectionFailed, 422},
		{xerror.KindTimeout, 408},
		{xerror.KindUnavailable, 422},
		{xerror.KindUnsupported, 422},
		{xerror.KindForbidden, 403},
		{xerror.KindInternal, 500},
		{xerror.Kind(0), 500},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.status, tt.kind.HTTPStatus())
		})
	}
}

// ---------------------------------------------------------------------------
// Entity accessors
// ---------------------------------------------------------------------------

func TestEntity_Code(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "DATA_SOURCE", xerror.DataSource.Code())
	assert.Equal(t, "USER", xerror.User.Code())
}

func TestEntity_Label(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "data source", xerror.DataSource.Label())
	assert.Equal(t, "user", xerror.User.Label())
}
