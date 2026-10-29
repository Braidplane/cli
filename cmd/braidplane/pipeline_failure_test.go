package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	api "github.com/Braidplane/cli/internal/apiclient"
)

func TestPipelineRunTextRetainsDataSourceDiagnostics(t *testing.T) {
	for _, reason := range []string{"data_source_credentials_failed", "data_source_permission_denied", "data_source_connection_failed", "data_source_timeout", "source_query_failed", "source_query_invalid", "source_schema_reference_invalid"} {
		t.Run(reason, func(t *testing.T) {
			var run api.PipelineRunRecord
			require.NoError(t, json.Unmarshal([]byte(`{"metadata":{"run_slug":"denied-reader","pipeline_slug":"orders","pipeline_version_spec_hash":"executed-hash","status":"failed","request_id":"request-1","source":{"type":"api","request_id":"request-1"}},"timeline":{"failure":{"step":"read-orders","kind":"data_source","side":"source","reason_code":"`+reason+`","data_source_slug":"reader","error":"Safe cause","action_hint":"Safe recovery guidance"}}}`), &run))
			var out bytes.Buffer
			require.NoError(t, writePipelineRunOutput(&out, &run, "text"))
			for _, value := range []string{reason, "reader", "read-orders", "Safe cause", "Safe recovery guidance", "executed-hash", "request-1"} {
				require.Contains(t, out.String(), value)
			}
		})
	}
}
