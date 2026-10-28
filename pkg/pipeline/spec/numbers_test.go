package spec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPipelineDynamicIntegersSurviveJSONAndYAMLRoundTrips(t *testing.T) {
	input := []byte(`{"metadata":{"slug":"exact","name":"Exact"},"steps":[{"kind":"query","slug":"fetch","query":{"data_source_ref":"source","operation_type":"sql","parameters":{"id":9007199254740993,"nested":[{"id":9007199254740993}],"fraction":1.25},"operation_inline":{"id":9007199254740993}}},{"kind":"sink","slug":"store","sink":{"input":"fetch","data_source_ref":"target","operation_type":"sql","operation_inline":{"id":9007199254740993},"config":{"id":9007199254740993}}}]}`)
	pipeline, err := Load("exact.pipeline.json", input)
	require.NoError(t, err)
	encoded, err := json.Marshal(pipeline)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"id":9007199254740993`)
	yamlData, err := yaml.Marshal(pipeline)
	require.NoError(t, err)
	reloaded, err := Load("exact.pipeline.yaml", yamlData)
	require.NoError(t, err)
	require.Equal(t, int64(9007199254740993), reloaded.Steps[0].Query.Parameters["id"])
	require.Equal(t, int64(9007199254740993), reloaded.Steps[0].Query.Parameters["nested"].([]any)[0].(map[string]any)["id"])
	require.Equal(t, int64(9007199254740993), reloaded.Steps[0].Query.OperationInline["id"])
	require.Equal(t, int64(9007199254740993), reloaded.Steps[1].Sink.OperationInline["id"])
	require.Equal(t, int64(9007199254740993), reloaded.Steps[1].Sink.Config["id"])
	require.InDelta(t, 1.25, reloaded.Steps[0].Query.Parameters["fraction"], 0)
	actual, err := json.Marshal(reloaded)
	require.NoError(t, err)
	require.JSONEq(t, string(input), string(actual))
}

func TestRuntimeNumbersBoundariesAndStrictFields(t *testing.T) {
	for _, token := range []string{"-9223372036854775808", "9223372036854775807", "18446744073709551615", "9007199254740993", "-9007199254740993", "100", "1.25"} {
		t.Run(token, func(t *testing.T) {
			pipeline, err := Load("pipeline.json", []byte(`{"steps":[{"query":{"parameters":{"value":`+token+`}}}]}`))
			require.NoError(t, err)
			encoded, err := json.Marshal(pipeline.Steps[0].Query.Parameters)
			require.NoError(t, err)
			require.Equal(t, `{"value":`+token+`}`, string(encoded))
		})
	}
	for _, input := range []string{`{"value":18446744073709551616}`, `{"value":-9223372036854775809}`, `{"value":1e999}`} {
		for _, field := range []string{`"query":{"parameters":`, `"sink":{"config":`} {
			_, err := Load("pipeline.json", []byte(`{"steps":[{`+field+input+`}}]}`))
			require.Error(t, err)
		}
	}
	for _, field := range []string{"query", "sink"} {
		_, err := Load("pipeline.json", []byte(`{"steps":[{"`+field+`":{"unknown":true}}]}`))
		require.Error(t, err)
	}
}
