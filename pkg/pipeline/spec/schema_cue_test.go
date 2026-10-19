package spec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// CUE vet subprocesses are intentionally serialized to avoid CI runner CPU contention.
func TestPipelineSpecCUESchemaValidationBoundaries(t *testing.T) {
	validSpecs := map[string]string{
		"AcceptsImmutableODCSContractRefs": `{
  "metadata": {"slug": "odcs-contract-refs", "name": "ODCS contract refs"},
  "steps": [{
    "kind": "query", "slug": "fetch-users",
    "query": {"data_source_ref": "warehouse", "operation_type": "sql", "operation_ref": "list-users", "output_contract_ref": {"contract_slug": "users", "revision_slug": "v1", "schema_object_id": "users-row"}}
  }, {
    "kind": "transform", "slug": "shape-users",
    "transform": {"inputs": {"input": "fetch-users"}, "sql": "SELECT id FROM input", "output_kind": "rows", "input_contract_ref": {"contract_slug": "users", "revision_slug": "v1", "schema_object_id": "users-row"}, "output_contract_ref": {"contract_slug": "users", "revision_slug": "v1", "schema_object_id": "shaped-users"}}
  }, {
    "kind": "sink", "slug": "write-users",
    "sink": {"input": "shape-users", "data_source_ref": "warehouse", "operation_type": "sql", "operation_ref": "upsert-users", "idempotency_class": "idempotent_with_key", "input_contract_ref": {"contract_slug": "users", "revision_slug": "v1", "schema_object_id": "shaped-users"}}
  }]
}`,
		"AcceptsRunScopedJSONInput": `{
  "metadata": {"slug": "event-pipeline", "name": "Event Pipeline"},
  "steps": [{
    "kind": "input",
    "slug": "event",
    "input": {
      "output_contract": {
        "name": "event",
        "representation": "json",
        "root_shape": "object",
        "fields": [{"name": "id", "type": "string", "required": true}]
      }
    }
  }]
}`,
		"AcceptsWorkspaceReadAndUpdate": `{
 "metadata":{"slug":"workspace","name":"Workspace"},
 "steps":[
  {"kind":"query","slug":"read-sheet","query":{"data_source_ref":"sheet","operation_type":"google_workspace","operation_inline":{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:B3"}}},
  {"kind":"sink","slug":"update-sheet","sink":{"input":"read-sheet","data_source_ref":"sheet","operation_type":"google_workspace","operation_inline":{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"D2:E3","columns":["a","b"],"rows":"{{input}}"},"idempotency_class":"naturally_idempotent"}}
 ]
}`,

		"AcceptsRowUniqueDatetimeAndInlineOperation": `{
  "metadata": {"slug": "valid-contract", "name": "Valid Contract"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_inline": {"sql": "select id, created_at from users"},
      "output_contract": {
        "representation": "rows",
        "fields": [
          {"name": "id", "type": "integer", "validations": {"unique": true, "min": 1}},
          {"name": "created_at", "type": "datetime", "validations": {"min": "2026-01-01T00:00:00Z"}}
        ]
      }
    }
  }]
}`,
		"AcceptsRowUniqueDatetimeAndOperationRef": `{
  "metadata": {"slug": "valid-contract", "name": "Valid Contract"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [
          {"name": "id", "type": "integer", "validations": {"unique": true, "min": 1}},
          {"name": "created_at", "type": "datetime", "validations": {"min": "2026-01-01T00:00:00Z"}}
        ]
      }
    }
  }]
}`,
		"AcceptsBigQuerySourceAndSinkOperations": `{
  "metadata": {"slug": "bigquery-warehouse-sync", "name": "BigQuery Warehouse Sync"},
  "steps": [{
    "kind": "query",
    "slug": "read-orders",
    "query": {
      "data_source_ref": "warehouse",
      "operation_type": "bigquery",
      "operation_inline": {
        "type": "bigquery",
        "operation": "query",
        "sql": "SELECT id, updated_at FROM analytics.orders WHERE updated_at > {{runtime.incremental.lower_bound.value}}"
      },
      "incremental": {
        "checkpoint_kind": "watermark",
        "field": "updated_at"
      },
      "output_contract": {
        "representation": "rows",
        "fields": [
          {"name": "id", "type": "string"},
          {"name": "updated_at", "type": "datetime"}
        ]
      }
    }
  }, {
    "kind": "sink",
    "slug": "write-orders",
    "sink": {
      "input": "read-orders",
      "data_source_ref": "warehouse",
      "operation_type": "bigquery",
      "operation_ref": "insert-orders",
      "idempotency_class": "idempotent_with_key",
      "input_contract": {
        "representation": "rows",
        "fields": [
          {"name": "id", "type": "string"},
          {"name": "updated_at", "type": "datetime"}
        ]
      }
    }
  }]
}`,
		"AcceptsHTTPMultipartQueryContract": `{
  "metadata": {"slug": "http-form-query", "name": "HTTP Form Query"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-form",
    "query": {
      "data_source_ref": "acme-http",
      "operation_type": "http",
      "operation_ref": "download-form",
      "output_contract": {
        "representation": "multipart_form",
        "fields": [
          {"name": "email", "type": "string"}
        ]
      }
    }
  }]
}`,
		"AcceptsExternalFieldPathSegments": `{
  "metadata": {"slug": "external-field-paths", "name": "External Field Paths"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [
          {"name": "user-id", "type": "string"},
          {"name": "2fa.enabled", "type": "boolean"}
        ]
      }
    }
  }]
}`,
		"AcceptsMongoDBInferredJSONFieldValidation": `{
  "metadata": {"slug": "mongo-json-validation", "name": "Mongo JSON Validation"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-mongodb",
      "operation_type": "mongodb",
      "operation_ref": "list-users",
      "output_contract": {
        "fields": [
          {"name": "email", "type": "string", "validations": {"format": "email"}}
        ]
      }
    }
  }]
}`,
	}

	invalidSpecs := map[string]string{
		"RejectsLatestOrInlineODCSContractReference": `{
  "metadata": {"slug": "odcs-contract-refs", "name": "ODCS contract refs"},
  "steps": [{
    "kind": "query", "slug": "fetch-users",
    "query": {"data_source_ref": "warehouse", "operation_type": "sql", "operation_ref": "list-users", "output_contract": {"representation": "rows", "fields": [{"name": "id", "type": "string"}]}, "output_contract_ref": {"contract_slug": "users", "revision_slug": "latest", "schema_object_id": "users-row"}}
  }]
}`,
		"RejectsNonJSONRunInput": `{
  "metadata": {"slug": "event-pipeline", "name": "Event Pipeline"},
  "steps": [{
    "kind": "input",
    "slug": "event",
    "input": {
      "output_contract": {
        "name": "event",
        "representation": "rows",
        "fields": [{"name": "id", "type": "string"}]
      }
    }
  }]
}`,
		"RejectsEmptyInlineOperation": `{
  "metadata": {"slug": "empty-inline", "name": "Empty Inline"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_inline": {}
    }
  }]
}`,
		"RejectsSinkDataSourceSlug": `{
  "metadata": {"slug": "legacy-sink-data-source-slug", "name": "Legacy Sink Data Source Slug"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }, {
    "kind": "sink",
    "slug": "write-users",
    "sink": {
      "input": "fetch-users",
      "data_source_slug": "acme-mysql",
      "operation_type": "sql",
      "operation_ref": "upsert-users",
      "idempotency_class": "idempotent_with_key"
    }
  }]
}`,
		"RejectsLooseDatetimeBound": `{
  "metadata": {"slug": "loose-datetime", "name": "Loose Datetime"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{
          "name": "created_at",
          "type": "datetime",
          "validations": {"min": "tomorrow"}
        }]
      }
    }
  }]
}`,
		"RejectsImpossibleDatetimeBound": `{
  "metadata": {"slug": "impossible-datetime", "name": "Impossible Datetime"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{
          "name": "created_at",
          "type": "datetime",
          "validations": {"min": "2026-99-99T99:99:99Z"}
        }]
      }
    }
  }]
}`,
		"RejectsReversedIntegerBounds": `{
  "metadata": {"slug": "reversed-integer-bounds", "name": "Reversed Integer Bounds"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{
          "name": "score",
          "type": "integer",
          "validations": {"min": 10, "max": 1}
        }]
      }
    }
  }]
}`,
		"RejectsReversedStringLengthBounds": `{
  "metadata": {"slug": "reversed-string-length", "name": "Reversed String Length"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{
          "name": "email",
          "type": "string",
          "validations": {"min_length": 10, "max_length": 1}
        }]
      }
    }
  }]
}`,
		"RejectsReversedDatetimeBounds": `{
  "metadata": {"slug": "reversed-datetime-bounds", "name": "Reversed Datetime Bounds"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{
          "name": "created_at",
          "type": "datetime",
          "validations": {
            "min": "2026-01-02T00:00:00Z",
            "max": "2026-01-01T00:00:00Z"
          }
        }]
      }
    }
  }]
}`,
		"RejectsUniqueOnJSONContract": `{
  "metadata": {"slug": "json-unique", "name": "JSON Unique"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-http",
      "operation_type": "http",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "json",
        "fields": [{
          "name": "email",
          "type": "string",
          "validations": {"unique": true}
        }]
      }
    }
  }]
}`,
		"RejectsSQLMultipartQueryContract": `{
  "metadata": {"slug": "sql-form-query", "name": "SQL Form Query"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "multipart_form",
        "fields": [{"name": "email", "type": "string"}]
      }
    }
  }]
}`,
		"RejectsFTPMultipartSinkContract": `{
  "metadata": {"slug": "ftp-form-sink", "name": "FTP Form Sink"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }, {
    "kind": "sink",
    "slug": "write-users",
    "sink": {
                    "idempotency_class": "idempotent_with_key",
      "input": "fetch-users",
      "data_source_ref": "acme-ftp",
      "operation_type": "ftp",
      "operation_ref": "upload-users",
      "input_contract": {
        "representation": "multipart_form",
        "fields": [{"name": "email", "type": "string"}]
      }
    }
  }]
}`,
		"RejectsFileFieldValidations": `{
  "metadata": {"slug": "file-field-validations", "name": "File Field Validations"},
  "steps": [{
    "kind": "query",
    "slug": "download-users",
    "query": {
      "data_source_ref": "acme-ftp",
      "operation_type": "ftp",
      "operation_ref": "download-users",
      "output_contract": {
        "representation": "file",
        "fields": [{
          "name": "digest",
          "type": "string",
          "validations": {"format": "uuid"}
        }]
      }
    }
  }]
}`,
		"RejectsMultipartFormFieldValidations": `{
  "metadata": {"slug": "form-field-validations", "name": "Form Field Validations"},
  "steps": [{
    "kind": "query",
    "slug": "download-users",
    "query": {
      "data_source_ref": "acme-http",
      "operation_type": "http",
      "operation_ref": "download-users",
      "output_contract": {
        "representation": "multipart_form",
        "fields": [{
          "name": "email",
          "type": "string",
          "validations": {"format": "email"}
        }]
      }
    }
  }]
}`,
		"RejectsArrayOnlyFieldPath": `{
  "metadata": {"slug": "array-only-field-path", "name": "Array Only Field Path"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{"name": "[]", "type": "string"}]
      }
    }
  }]
}`,
		"RejectsNestedArrayOnlyFieldPath": `{
  "metadata": {"slug": "nested-array-only-field-path", "name": "Nested Array Only Field Path"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users",
      "output_contract": {
        "representation": "rows",
        "fields": [{"name": "profile.[].sku", "type": "string"}]
      }
    }
  }]
}`,
		"RejectsMongoDBInferredJSONUniqueValidation": `{
  "metadata": {"slug": "mongo-json-unique", "name": "Mongo JSON Unique"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-mongodb",
      "operation_type": "mongodb",
      "operation_ref": "list-users",
      "output_contract": {
        "fields": [{
          "name": "email",
          "type": "string",
          "validations": {"unique": true}
        }]
      }
    }
  }]
}`,
		"RejectsTransformFileInputContract": `{
  "metadata": {"slug": "transform-file-contract", "name": "Transform File Contract"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-files",
      "operation_type": "ftp",
      "operation_ref": "download-users"
    }
  }, {
    "kind": "transform",
    "slug": "shape-users",
    "transform": {
      "inputs": {"input": "fetch-users"},
      "sql": "SELECT * FROM input",
      "output_kind": "rows",
      "input_contract": {
        "representation": "file"
      },
      "output_contract": {
        "representation": "rows",
        "fields": [{"name": "id", "type": "integer"}]
      }
    }
  }]
}`,
		"RejectsTransformMultipartOutputContract": `{
  "metadata": {"slug": "transform-form-contract", "name": "Transform Form Contract"},
  "steps": [{
    "kind": "query",
    "slug": "fetch-users",
    "query": {
      "data_source_ref": "acme-postgres",
      "operation_type": "sql",
      "operation_ref": "list-users"
    }
  }, {
    "kind": "transform",
    "slug": "shape-users",
    "transform": {
      "inputs": {"input": "fetch-users"},
      "sql": "SELECT * FROM input",
      "output_kind": "rows",
      "input_contract": {
        "representation": "rows",
        "fields": [{"name": "id", "type": "integer"}]
      },
      "output_contract": {
        "representation": "multipart_form",
        "fields": [{"name": "payload", "type": "string"}]
      }
    }
  }]
}`,
	}

	// CUE vet is a heavyweight external process; keep these cases sequential so
	// CI runners do not fan out several compiler/validator subprocesses at once.
	for name, spec := range validSpecs {
		t.Run(name, func(t *testing.T) {
			output, err := runCUEVetForPipelineSpec(t, spec)
			require.NoError(t, err, output)
		})
	}

	// CUE vet subprocesses are intentionally serialized to avoid CI runner CPU contention.
	for name, spec := range invalidSpecs {
		t.Run(name, func(t *testing.T) {
			_, err := runCUEVetForPipelineSpec(t, spec)
			require.Error(t, err)
		})
	}
}

func runCUEVetForPipelineSpec(t *testing.T, spec string) (string, error) {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "pipeline.json")
	require.NoError(t, os.WriteFile(specPath, []byte(spec), 0o600))

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"go",
		"tool",
		"cue",
		"vet",
		"-c",
		"pkg/pipeline/spec/schema.cue",
		"-d",
		"#Pipeline",
		specPath,
	)
	cmd.Dir = repoRoot

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestPipelineSpecDecimalDeclaration(t *testing.T) {
	for _, test := range []struct {
		field string
		valid bool
	}{
		{`{"name":"price","type":"decimal","precision":38,"scale":2}`, true},
		{`{"name":"price","type":"decimal","precision":39,"scale":2}`, false},
		{`{"name":"price","type":"decimal","precision":5,"scale":6}`, false},
		{`{"name":"price","type":"decimal"}`, false},
		{`{"name":"price","type":"number","precision":38,"scale":2}`, false},
	} {
		spec := `{"metadata":{"slug":"prices","name":"Prices"},"steps":[{"kind":"query","slug":"read-prices","query":{"data_source_ref":"prices-source","operation_type":"sql","operation_ref":"read-prices","output_contract":{"representation":"rows","fields":[` + test.field + `]}}}]}`
		output, err := runCUEVetForPipelineSpec(t, spec)
		if test.valid {
			require.NoError(t, err, output)
		} else {
			require.Error(t, err)
		}
	}
}
