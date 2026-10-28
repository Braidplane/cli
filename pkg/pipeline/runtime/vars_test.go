package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/must"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
)

func TestResolvePipelineSubstitutesEnvironmentVariablesInRuntimeFields(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-db"),
					OperationType:  "sql",
					OperationInline: map[string]any{
						"statement": "select * from orders where bucket = {{runtime.env.s3_prefix}}",
					},
					Parameters: map[string]any{
						"prefix":        "{{runtime.env.s3_prefix}}",
						"limit":         "{{runtime.env.batch_size}}",
						"kept":          "{{runtime.run.date}}",
						"path":          "{{runtime.env.step_like_path}}",
						"embedded_path": "{{runtime.env.step_like_prefix}}/orders",
					},
					Incremental: &pipelinespec.IncrementalSourceContract{
						LookbackWindow: "{{runtime.env.lookback_hours}}h",
					},
				},
			},
			{
				Kind: "sink",
				Slug: must.Slug("write-orders"),
				Sink: &pipelinespec.SinkSpec{
					IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
					DataSourceSlug:   must.Slug("warehouse"),
					OperationType:    "http",
					Config: map[string]any{
						"path":      "/{{runtime.env.s3_prefix}}/orders",
						"step_path": "{{runtime.env.step_like_path}}",
						"nested": map[string]any{
							"enabled": "{{runtime.env.enabled}}",
							"path":    "{{runtime.env.step_like_path}}",
						},
					},
				},
			},
		},
	}

	err := ResolvePipeline(pipeline, []model.RuntimeVariable{
		{Key: "s3_prefix", ValueType: model.RuntimeVariableValueTypeText, Value: "dev/dir"},
		{Key: "batch_size", ValueType: model.RuntimeVariableValueTypeInteger, Value: "250"},
		{Key: "enabled", ValueType: model.RuntimeVariableValueTypeBoolean, Value: "true"},
		{Key: "lookback_hours", ValueType: model.RuntimeVariableValueTypeInteger, Value: "24"},
		{Key: "step_like_path", ValueType: model.RuntimeVariableValueTypeText, Value: "$steps.archive/path"},
		{Key: "step_like_prefix", ValueType: model.RuntimeVariableValueTypeText, Value: "$steps.archive"},
	})
	require.NoError(t, err)

	require.Equal(t, "select * from orders where bucket = {{runtime.env.s3_prefix}}", pipeline.Steps[0].Query.OperationInline["statement"])
	require.Equal(t, "dev/dir", pipeline.Steps[0].Query.Parameters["runtime.env.s3_prefix"])
	require.Equal(t, "dev/dir", pipeline.Steps[0].Query.Parameters["prefix"])
	require.Equal(t, int64(250), pipeline.Steps[0].Query.Parameters["limit"])
	require.Equal(t, "{{runtime.run.date}}", pipeline.Steps[0].Query.Parameters["kept"])
	require.Equal(t, LiteralString("$steps.archive/path"), pipeline.Steps[0].Query.Parameters["path"])
	require.Equal(t, "$steps.archive/orders", LiteralValue(pipeline.Steps[0].Query.Parameters["embedded_path"]))
	require.Equal(t, "24h", pipeline.Steps[0].Query.Incremental.LookbackWindow)
	require.Equal(t, "/dev/dir/orders", pipeline.Steps[1].Sink.Config["path"])
	require.Equal(t, LiteralString("$steps.archive/path"), pipeline.Steps[1].Sink.Config["step_path"])
	require.Equal(t, true, pipeline.Steps[1].Sink.Config["nested"].(map[string]any)["enabled"])
	require.Equal(t, LiteralString("$steps.archive/path"), pipeline.Steps[1].Sink.Config["nested"].(map[string]any)["path"])
}

func TestResolvePipelineInputSubstitutesPerRunValuesInRuntimeFields(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-db"),
					OperationType:  "http",
					OperationInline: map[string]any{
						"path": "/customers/{{runtime.input.customer_id}}/orders",
						"body": map[string]any{
							"payload": "{{runtime.input.payload}}",
						},
					},
					Parameters: map[string]any{
						"customer_id":  "{{runtime.input.customer_id}}",
						"limit":        "{{runtime.input.limit}}",
						"dry_run":      "{{runtime.input.dry_run}}",
						"literal_ref":  "{{runtime.input.step_ref}}",
						"literal_date": "{{runtime.input.as_of}}",
					},
				},
			},
			{
				Kind: "sink",
				Slug: must.Slug("write-orders"),
				Sink: &pipelinespec.SinkSpec{
					IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
					DataSourceSlug:   must.Slug("warehouse"),
					OperationType:    "http",
					Config: map[string]any{
						"path": "/exports/{{runtime.input.customer_id}}",
					},
				},
			},
		},
	}

	err := ResolvePipelineInput(pipeline, map[string]any{
		"customer_id": "cus_123",
		"limit":       100,
		"dry_run":     true,
		"step_ref":    "$steps.fetch-orders.rows",
		"as_of":       "{{runtime.run.date}}",
		"payload": map[string]any{
			"tier": "enterprise",
		},
	})
	require.NoError(t, err)

	require.Equal(t, "/customers/{{runtime.input.customer_id}}/orders", pipeline.Steps[0].Query.OperationInline["path"])
	require.Equal(t, map[string]any{"tier": "enterprise"}, pipeline.Steps[0].Query.Parameters["runtime.input.payload"])
	require.Equal(t, "cus_123", pipeline.Steps[0].Query.Parameters["customer_id"])
	require.Equal(t, 100, pipeline.Steps[0].Query.Parameters["limit"])
	require.Equal(t, true, pipeline.Steps[0].Query.Parameters["dry_run"])
	require.Equal(t, LiteralString("$steps.fetch-orders.rows"), pipeline.Steps[0].Query.Parameters["literal_ref"])
	require.Equal(t, LiteralString("{{runtime.run.date}}"), pipeline.Steps[0].Query.Parameters["literal_date"])
	require.Equal(t, "/exports/cus_123", pipeline.Steps[1].Sink.Config["path"])
}

func TestResolvePipelineSecretsSubstitutesWriteOnlyEnvironmentSecrets(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-api"),
					OperationType:  "http",
					OperationInline: map[string]any{
						"headers": map[string]any{
							"Authorization": "Bearer {{runtime.secrets.API_TOKEN}}",
						},
					},
					Parameters: map[string]any{
						"token":        "{{runtime.secrets.API_TOKEN}}",
						"literal_ref":  "{{runtime.secrets.STEP_REF}}",
						"kept":         "{{runtime.env.s3_prefix}}",
						"literal_date": "{{runtime.secrets.RUNTIME_TEXT}}",
					},
				},
			},
			{
				Kind: "sink",
				Slug: must.Slug("write-orders"),
				Sink: &pipelinespec.SinkSpec{
					IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
					DataSourceSlug:   must.Slug("warehouse"),
					OperationType:    "http",
					Config: map[string]any{
						"signature": "{{runtime.secrets.WEBHOOK_SECRET}}",
					},
				},
			},
		},
	}

	err := ResolvePipelineSecrets(pipeline, []model.RuntimeSecretValue{
		{Key: "API_TOKEN", Value: "ghp_super_secret"},
		{Key: "WEBHOOK_SECRET", Value: "signed-secret"},
		{Key: "STEP_REF", Value: "$steps.fetch-orders.rows"},
		{Key: "RUNTIME_TEXT", Value: "{{runtime.run.datetime}}"},
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer {{runtime.secrets.API_TOKEN}}", pipeline.Steps[0].Query.OperationInline["headers"].(map[string]any)["Authorization"])
	require.Equal(t, "ghp_super_secret", pipeline.Steps[0].Query.Parameters["runtime.secrets.API_TOKEN"])
	require.Equal(t, "ghp_super_secret", pipeline.Steps[0].Query.Parameters["token"])
	require.Equal(t, LiteralString("$steps.fetch-orders.rows"), pipeline.Steps[0].Query.Parameters["literal_ref"])
	require.Equal(t, "{{runtime.env.s3_prefix}}", pipeline.Steps[0].Query.Parameters["kept"])
	require.Equal(t, LiteralString("{{runtime.run.datetime}}"), pipeline.Steps[0].Query.Parameters["literal_date"])
	require.Equal(t, "signed-secret", pipeline.Steps[1].Sink.Config["signature"])
}

func TestResolvePipelineSecretsReturnsStepErrorForMissingSecret(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-api"),
					OperationType:  "http",
					Parameters: map[string]any{
						"headers": []any{
							"{{runtime.secrets.WEBHOOK_SECRET}}",
							"{{runtime.secrets.API_TOKEN}}",
						},
					},
				},
			},
		},
	}

	err := ResolvePipelineSecrets(pipeline, []model.RuntimeSecretValue{
		{Key: "WEBHOOK_SECRET", Value: "signed-secret"},
	})

	require.ErrorContains(t, err, `step[0] "fetch-orders" query parameters`)
	require.ErrorContains(t, err, "environment secret API_TOKEN is not defined")
	require.NotContains(t, err.Error(), "signed-secret")
}

func TestEnvironmentSecretReferenceDetection(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "sink",
				Slug: must.Slug("write-orders"),
				Sink: &pipelinespec.SinkSpec{
					IdempotencyClass: pipelinespec.SinkIdempotencyClassIdempotentWithKey,
					DataSourceSlug:   must.Slug("warehouse"),
					OperationType:    "http",
					Config: map[string]any{
						"headers": []any{
							"Bearer {{runtime.secrets.API_TOKEN}}",
						},
					},
				},
			},
		},
	}

	require.True(t, IsEnvironmentSecretReference(SecretReference("API_TOKEN")))
	require.False(t, IsEnvironmentSecretReference("Bearer "+SecretReference("API_TOKEN")))
	require.True(t, HasEnvironmentSecretReference("Bearer "+SecretReference("API_TOKEN")))
	require.True(t, HasEnvironmentSecretReferenceInPipeline(pipeline))
	require.False(t, HasEnvironmentSecretReferenceInPipeline(nil))
}

func TestResolvePipelineInputReturnsStepErrorForMissingInput(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-db"),
					OperationType:  "sql",
					Parameters: map[string]any{
						"customer_id": "{{runtime.input.customer_id}}",
					},
				},
			},
		},
	}

	err := ResolvePipelineInput(pipeline, nil)
	require.ErrorContains(t, err, `step[0] "fetch-orders" query parameters`)
	require.ErrorContains(t, err, "runtime input customer_id is not defined")
}

func TestRuntimeInputKeysAreValidatedAndSortedForMetadata(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateInput(map[string]any{
		"z_key":  true,
		"a_key":  "value",
		"author": "alice",
	}))
	require.Equal(t, []string{"input.a_key", "input.author", "input.z_key"}, InputParameterKeys(map[string]any{
		"z_key":  true,
		"a_key":  "value",
		"author": "alice",
	}))

	require.ErrorContains(t, ValidateInput(map[string]any{"bad-key": "value"}), "runtime input key must match")
	require.ErrorContains(t, ValidateInput(map[string]any{"api_token": "value"}), "runtime input values are non-secret")
	require.ErrorContains(t, ValidateInput(map[string]any{"my_api_key": "value"}), "runtime input values are non-secret")
	require.ErrorContains(t, ValidateInput(map[string]any{"apiToken": "value"}), "runtime input values are non-secret")
	require.ErrorContains(t, ValidateInput(map[string]any{"myApiKey": "value"}), "runtime input values are non-secret")
	require.ErrorContains(t, ValidateInput(map[string]any{"APIKey": "value"}), "runtime input values are non-secret")
}

func TestResolvePipelineReturnsStepErrorForMissingVariable(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-db"),
					OperationType:  "sql",
					Parameters: map[string]any{
						"prefix": "{{runtime.env.missing_prefix}}",
					},
				},
			},
		},
	}

	err := ResolvePipeline(pipeline, nil)
	require.ErrorContains(t, err, `step[0] "fetch-orders" query parameters`)
	require.ErrorContains(t, err, "runtime variable missing_prefix is not defined")
}

func TestResolvePipelineValidatesResolvedLookbackWindow(t *testing.T) {
	t.Parallel()

	pipeline := &pipelinespec.Pipeline{
		Metadata: pipelinespec.Metadata{Slug: must.Slug("orders"), Name: "Orders"},
		Steps: []pipelinespec.Step{
			{
				Kind: "query",
				Slug: must.Slug("fetch-orders"),
				Query: &pipelinespec.QueryRef{
					DataSourceSlug: must.Slug("orders-db"),
					OperationType:  "sql",
					OperationRef:   must.Slug("list-orders"),
					Incremental: &pipelinespec.IncrementalSourceContract{
						LookbackWindow: "{{runtime.env.lookback_window}}",
					},
				},
			},
		},
	}

	err := ResolvePipeline(pipeline, []model.RuntimeVariable{
		{Key: "lookback_window", ValueType: model.RuntimeVariableValueTypeText, Value: "eventually"},
	})

	require.ErrorContains(t, err, `step[0] "fetch-orders" query incremental.lookback_window`)
	require.ErrorContains(t, err, "lookback_window must resolve to a positive duration")
}

func TestEnvironmentKeysMatchExecutableSubstitutions(t *testing.T) {
	t.Parallel()
	pipeline := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Name: "{{runtime.env.IGNORED}}"}, Steps: []pipelinespec.Step{
		{Query: &pipelinespec.QueryRef{Parameters: map[string]any{"a": "{{ runtime.env.CUSTOMER }}", "b": []any{"{{runtime.env.DATE}}", "{{runtime.secrets.TOKEN}}"}}, OperationInline: map[string]any{"sql": "{{runtime.input.VALUE}} {{runtime.env.CUSTOMER}}"}}},
		{Sink: &pipelinespec.SinkSpec{Config: map[string]any{"path": "{{runtime.env.PREFIX}}"}}},
	}}
	require.Equal(t, []string{"CUSTOMER", "DATE", "PREFIX"}, EnvironmentKeys(pipeline))
	require.Empty(t, EnvironmentKeys(nil))
}
