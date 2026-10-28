package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	transformsql "github.com/Braidplane/cli/pkg/pipeline/transform/sql"
)

func TestPipelineMappingCLIAuthorsCanonicalSQLWithoutNetwork(t *testing.T) {
	t.Setenv("BRAIDPLANE_SERVER_URL", "http://127.0.0.1:1/api")
	t.Setenv("BRAIDPLANE_PAT", "fixture-must-not-be-used")
	sqlFile := writeMappingTestFile(t, "mapping.sql", "SELECT item_code AS sku, 'PLN' AS currency FROM input")
	original, err := os.ReadFile(sqlFile)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return runWithRuntime(append([]string{"braidplane", "pipeline", "mapping"}, args...), &stdout, &stderr,
			cliRuntime{configPath: filepath.Join(t.TempDir(), "missing-config.toml")})
	}
	require.NoError(t, run("inspect", "--file", sqlFile))
	var mapping transformsql.FieldMapping
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &mapping))
	require.Equal(t, "sku", mapping.Fields[0].Name)
	mapping.Fields = []transformsql.MappingField{mapping.Fields[1], mapping.Fields[0]}
	raw, err := json.Marshal(mapping)
	require.NoError(t, err)
	editingFile := writeMappingTestFile(t, "editing.json", string(raw))
	require.NoError(t, run("render", "--file", editingFile))
	canonical, err := transformsql.ReadFieldMapping(stdout.String())
	require.NoError(t, err)
	require.Equal(t, mapping.Fields, canonical.Fields)
	require.Empty(t, stderr.String())
	after, err := os.ReadFile(sqlFile)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestPipelineMappingCLISuggestionsAndContractDiagnostics(t *testing.T) {
	source := writeMappingTestFile(t, "source.json", `{"name":"supplier","fields":[{"name":"sku","type":"string","required":true},{"name":"price","type":"string","required":true}]}`)
	target := writeMappingTestFile(t, "target.json", `{"name":"catalog","fields":[{"name":"sku","type":"string","required":true},{"name":"price","type":"decimal","precision":18,"scale":2,"required":true},{"name":"currency","type":"string","required":true}]}`)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		args = append(args, "--input-contract", source, "--output-contract", target)
		return runWithRuntime(append([]string{"braidplane", "pipeline", "mapping"}, args...), &stdout, &stderr, cliRuntime{})
	}
	require.NoError(t, run("suggest"))
	var mapping transformsql.FieldMapping
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &mapping))
	require.Equal(t, []transformsql.MappingField{{Name: "sku", Expression: "`sku`"}}, mapping.Fields)
	for _, test := range []struct {
		name, sql string
		valid     bool
	}{
		{"missing required", "SELECT sku, CAST(price AS DECIMAL(18,2)) AS price FROM input", false},
		{"incompatible type", "SELECT sku, price, 'PLN' AS currency FROM input", false},
		{"complete shape", "SELECT sku, CAST(price AS DECIMAL(18,2)) AS price, 'PLN' AS currency FROM input", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := writeMappingTestFile(t, "mapping.sql", test.sql)
			err := run("check", "--file", file)
			var result struct {
				Valid       bool                `json:"valid"`
				Scope       string              `json:"scope"`
				Diagnostics []map[string]string `json:"diagnostics"`
			}
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.Equal(t, test.valid, result.Valid)
			if test.valid {
				require.NoError(t, err)
				require.Equal(t, "explicit_contract_shapes", result.Scope)
			} else {
				require.Error(t, err, "failed checks must produce a failure exit path")
				require.NotEmpty(t, result.Diagnostics[0]["code"])
				require.NotEmpty(t, result.Diagnostics[0]["message"])
			}
		})
	}
}

func TestPipelineMappingCLIRejectsUnknownAndUnavailableInputs(t *testing.T) {
	for _, raw := range []string{
		`{"input":"input","fields":[{"name":"sku","expression":"id"}],"unknown":true}`,
		`{"input":"input","fields":[{"name":"sku","expression":"id"}]} {}`,
		`{"input":"input","fields":[]}`,
	} {
		file := writeMappingTestFile(t, "editing.json", raw)
		var stdout, stderr bytes.Buffer
		err := runWithArgs([]string{"braidplane", "pipeline", "mapping", "render", "--file", file}, &stdout, &stderr)
		require.Error(t, err)
		require.Contains(t, stdout.String(), `"valid": false`)
	}
	file := writeMappingTestFile(t, "unavailable.json", "{}")
	var stdout, stderr bytes.Buffer
	err := runWithArgs([]string{"braidplane", "pipeline", "mapping", "suggest", "--input-contract", file, "--output-contract", file}, &stdout, &stderr)
	require.Error(t, err)
	require.Contains(t, stdout.String(), `"valid": false`)
	advanced := "SELECT id FROM input WHERE id > 0"
	file = writeMappingTestFile(t, "advanced.sql", advanced)
	stdout.Reset()
	err = runWithArgs([]string{"braidplane", "pipeline", "mapping", "inspect", "--file", file}, &stdout, &stderr)
	require.Error(t, err)
	require.Contains(t, stdout.String(), "PIPELINE_FIELD_MAPPING_UNSUPPORTED")
	unchanged, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, advanced, string(unchanged))
}

func TestPipelineMappingCLIFixtureHasSafeStepDiagnosticsAndFailureExit(t *testing.T) {
	t.Setenv("BRAIDPLANE_SERVER_URL", "http://127.0.0.1:1/api")
	source := writeMappingTestFile(t, "source.json", `{"name":"supplier","fields":[{"name":"sku","type":"string","required":true}]}`)
	target := writeMappingTestFile(t, "target.json", `{"name":"catalog","fields":[{"name":"sku","type":"string","required":true},{"name":"currency","type":"string","required":true}]}`)
	fixture := writeMappingTestFile(t, "fixture.json", `{"inputs":{"input":[{"sku":"private-customer-value"}]},"expected":[{"sku":"private-customer-value","currency":"PLN"}]}`)
	for _, test := range []struct {
		sql    string
		passed bool
	}{
		{"SELECT sku, 'PLN' AS currency FROM input", true},
		{"SELECT sku, 'EUR' AS currency FROM input", false},
		{"SELECT sku FROM input", false},
	} {
		file := writeMappingTestFile(t, "mapping.sql", test.sql)
		var stdout, stderr bytes.Buffer
		err := runWithArgs([]string{
			"braidplane", "pipeline", "mapping", "test", "--file", file, "--fixture", fixture,
			"--step", "map-products", "--input-contract", source, "--output-contract", target,
		}, &stdout, &stderr)
		var result struct {
			Passed      bool                `json:"passed"`
			Asserted    bool                `json:"asserted"`
			Diagnostics []map[string]string `json:"diagnostics"`
		}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.Equal(t, test.passed, result.Passed)
		if test.passed {
			require.NoError(t, err)
			require.True(t, result.Asserted)
		} else {
			require.Error(t, err)
			require.Equal(t, "map-products", result.Diagnostics[0]["step"])
			require.NotContains(t, stdout.String()+stderr.String()+err.Error(), "private-customer-value")
		}
	}
}

func writeMappingTestFile(t *testing.T, name, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestPipelineMappingCLIJSONCheckUsesSelectedSerialization(t *testing.T) {
	source := writeMappingTestFile(t, "source.json", `{"name":"supplier","fields":[{"name":"sku","type":"string","required":true}]}`)
	target := writeMappingTestFile(t, "target.json", `{"name":"catalog","fields":[{"name":"product","type":"object","required":true}]}`)
	file := writeMappingTestFile(t, "mapping.sql", "SELECT JSON_OBJECT('sku', sku) AS product FROM input")
	for _, kind := range []string{"rows", "json"} {
		var stdout, stderr bytes.Buffer
		err := runWithArgs([]string{
			"braidplane", "pipeline", "mapping", "check", "--file", file,
			"--input-contract", source, "--output-contract", target, "--output-kind", kind,
		}, &stdout, &stderr)
		if kind == "json" {
			require.NoError(t, err)
			require.Contains(t, stdout.String(), `"valid": true`)
		} else {
			require.Error(t, err)
			require.Contains(t, stdout.String(), `"valid": false`)
		}
	}
}
