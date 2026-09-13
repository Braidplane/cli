package xerror

// Entity identifies the domain object an error relates to.
type Entity struct {
	// code is the upper-case, underscore-separated token (e.g. "USER", "DATA_SOURCE").
	code string
	// label is the lower-case, space-separated display form (e.g. "user", "data source").
	label string
}

// Code returns the upper-case entity token.
func (e Entity) Code() string { return e.code }

// Label returns the lower-case display label.
func (e Entity) Label() string { return e.label }

// Domain entities.
var (
	User                = Entity{code: "USER", label: "user"}
	Organization        = Entity{code: "ORGANIZATION", label: "organization"}
	OrganizationSlug    = Entity{code: "ORGANIZATION_SLUG", label: "org slug"}
	Member              = Entity{code: "MEMBER", label: "member"}
	MemberInvitation    = Entity{code: "MEMBER_INVITATION", label: "member invitation"}
	Role                = Entity{code: "ROLE", label: "role"}
	APIKey              = Entity{code: "API_KEY", label: "api key"}
	PersonalAccessToken = Entity{code: "PERSONAL_ACCESS_TOKEN", label: "personal access token"}
	BillingProfile      = Entity{code: "BILLING_PROFILE", label: "billing profile"}
	DataSource          = Entity{code: "DATA_SOURCE", label: "data source"}
	DataSourceRef       = Entity{code: "DATA_SOURCE_REF", label: "data source ref"}
	DataSourceBinding   = Entity{code: "DATA_SOURCE_BINDING", label: "data source binding"}
	DataSourceService   = Entity{code: "DATA_SOURCE_SERVICE", label: "datasource service"}
	Project             = Entity{code: "PROJECT", label: "project"}
	Environment         = Entity{code: "ENVIRONMENT", label: "environment"}
	ProjectEnvironment  = Entity{
		code:  "PROJECT_ENVIRONMENT",
		label: "project environment",
	}
	RuntimeVariable   = Entity{code: "RUNTIME_VARIABLE", label: "runtime variable"}
	EnvironmentSecret = Entity{
		code:  "ENVIRONMENT_SECRET",
		label: "environment secret",
	}
	Event        = Entity{code: "EVENT", label: "event"}
	Notification = Entity{code: "NOTIFICATION", label: "notification"}
	AlertPolicy  = Entity{
		code:  "ALERT_POLICY",
		label: "alert policy",
	}
	AlertDestination = Entity{
		code:  "ALERT_DESTINATION",
		label: "alert destination",
	}
	Query          = Entity{code: "QUERY", label: "query"}
	QueryExecutor  = Entity{code: "QUERY_EXECUTOR", label: "query executor"}
	Permission     = Entity{code: "PERMISSION", label: "permission"}
	SchemaMetadata = Entity{code: "SCHEMA_METADATA", label: "schema metadata"}
	SchemaResult   = Entity{code: "SCHEMA_RESULT", label: "schema result"}
	JSON           = Entity{code: "JSON", label: "json"}
	UserMember     = Entity{code: "USER_MEMBER", label: "user member"}
	Slug           = Entity{code: "SLUG", label: "slug"}
	PasswordHash   = Entity{code: "PASSWORD_HASH", label: "password hash"}
	Cursor         = Entity{code: "CURSOR", label: "cursor"}
	ReplayRunSlug  = Entity{code: "REPLAY_RUN_SLUG", label: "replay run slug"}
	RunPersister   = Entity{code: "RUN_PERSISTER", label: "run persister"}
	RunStore       = Entity{code: "RUN_STORE", label: "run store"}
	SinkRuntime    = Entity{code: "SINK_RUNTIME", label: "sink runtime"}
)

// Fine-grained entities for uniqueness-conflict errors.
var (
	UserEmail = Entity{code: "USER_EMAIL", label: "user email"}
)

// Pipeline domain entities.
var (
	Pipeline                 = Entity{code: "PIPELINE", label: "pipeline"}
	PipelineDeployment       = Entity{code: "PIPELINE_DEPLOYMENT", label: "pipeline deployment"}
	PipelineDraft            = Entity{code: "PIPELINE_DRAFT", label: "pipeline draft"}
	PipelineReference        = Entity{code: "PIPELINE_REFERENCE", label: "pipeline reference"}
	PipelineVersion          = Entity{code: "PIPELINE_VERSION", label: "pipeline version"}
	PipelineRun              = Entity{code: "PIPELINE_RUN", label: "pipeline run"}
	PipelineSinkEffect       = Entity{code: "PIPELINE_SINK_EFFECT", label: "pipeline sink effect"}
	PipelineCheckpoint       = Entity{code: "PIPELINE_CHECKPOINT", label: "pipeline checkpoint"}
	PipelineTrigger          = Entity{code: "PIPELINE_TRIGGER", label: "pipeline trigger"}
	PipelineValidationResult = Entity{
		code:  "PIPELINE_VALIDATION_RESULT",
		label: "pipeline validation result",
	}
	DriftImpactReport = Entity{code: "DRIFT_IMPACT_REPORT", label: "drift impact report"}
)

// Repository domain entities.
var (
	RepoFile = Entity{code: "REPO_FILE", label: "repository file"}
)
