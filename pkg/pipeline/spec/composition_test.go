package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositionCanonicalSyntax(t *testing.T) {
	const document = `{"metadata":{"slug":"orders","name":"Orders"},"steps":[{"kind":"input","slug":"event","input":{"output_contract_ref":{"contract_slug":"orders","revision_slug":"v1","schema_object_id":"row","representation":"json","root_shape":"object"}}},{"kind":"transform","slug":"audit","transform":{"inputs":{"orders":"event"},"sql":"SELECT id FROM orders","output_kind":"rows","input_contracts":{"orders":{"contract":{"representation":"json","root_shape":"object","fields":[{"name":"id","type":"integer","required":true}]}}}}}]}`
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{name: "exact event reference and named requirement", valid: true},
		{name: "unknown input", change: func(transform map[string]any) {
			transform["input_contracts"].(map[string]any)["unknown"] = transform["input_contracts"].(map[string]any)["orders"]
		}},
		{name: "combined input conflict", change: func(transform map[string]any) {
			transform["input_contract"] = map[string]any{"fields": []any{map[string]any{"name": "id", "type": "integer"}}}
		}},
		{name: "empty entry", change: func(transform map[string]any) {
			transform["input_contracts"].(map[string]any)["orders"] = map[string]any{}
		}},
		{name: "file reference", change: func(transform map[string]any) {
			transform["input_contracts"].(map[string]any)["orders"] = map[string]any{"contract_ref": map[string]any{"contract_slug": "orders", "revision_slug": "v1", "schema_object_id": "row", "representation": "file"}}
		}},
		{name: "multipart reference", change: func(transform map[string]any) {
			transform["input_contracts"].(map[string]any)["orders"] = map[string]any{"contract_ref": map[string]any{"contract_slug": "orders", "revision_slug": "v1", "schema_object_id": "row", "representation": "multipart_form"}}
		}},
		{name: "inline reference conflict", change: func(transform map[string]any) {
			transform["input_contracts"].(map[string]any)["orders"].(map[string]any)["contract_ref"] = map[string]any{"contract_slug": "orders", "revision_slug": "v1", "schema_object_id": "row"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value map[string]any
			require.NoError(t, json.Unmarshal([]byte(document), &value))
			if tc.change != nil {
				tc.change(value["steps"].([]any)[1].(map[string]any)["transform"].(map[string]any))
			}
			raw, err := json.Marshal(value)
			require.NoError(t, err)
			output, err := runCUEVetForPipelineSpec(t, string(raw))
			if tc.valid {
				require.NoError(t, err, output)
				p, err := Load("orders.json", raw)
				require.NoError(t, err)
				require.True(t, p.UsesContractComposition())
			} else {
				require.Error(t, err, output)
			}
		})
	}
}
