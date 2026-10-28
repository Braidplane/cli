package sql

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestFieldMappingRoundTripUsesOrdinaryRuntime(t *testing.T) {
	t.Parallel()
	original := `SELECT item_code AS sku, description AS name,
 CAST(price AS DECIMAL(18,2)) AS unit_price, 'PLN' AS currency FROM input`
	mapping, err := ReadFieldMapping(original)
	require.NoError(t, err)
	require.Equal(t, "input", mapping.Input)
	require.Equal(t, []string{"sku", "name", "unit_price", "currency"}, mappingNames(mapping))
	encoded, err := json.Marshal(mapping)
	require.NoError(t, err)
	var reloaded FieldMapping
	require.NoError(t, json.Unmarshal(encoded, &reloaded))
	canonical, err := WriteFieldMapping(reloaded)
	require.NoError(t, err)
	roundTrip, err := ReadFieldMapping(canonical)
	require.NoError(t, err)
	require.Equal(t, mapping, roundTrip)
	input := map[string]*pipepayload.Payload{"input": {
		Representation: model.ContractRepresentationRows,
		Rows:           []map[string]any{{"item_code": "CHAIR-42", "description": "Oak chair", "price": "249.90", "supplier_notes": "omit"}},
	}}
	a, err := NewRuntime().Execute(Transform{SQL: original, OutputKind: model.TransformOutputKindRows}, input)
	require.NoError(t, err)
	b, err := NewRuntime().Execute(Transform{SQL: canonical, OutputKind: model.TransformOutputKindRows}, input)
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.NotContains(t, b.Rows[0], "supplier_notes")
	require.Equal(t, "249.90", b.Rows[0]["unit_price"].(interface{ String() string }).String())

	// Reordering and deleting projections must retain SQL column order.
	reloaded.Fields = []MappingField{reloaded.Fields[3], reloaded.Fields[0]}
	canonical, err = WriteFieldMapping(reloaded)
	require.NoError(t, err)
	b, err = NewRuntime().Execute(Transform{SQL: canonical}, input)
	require.NoError(t, err)
	require.Equal(t, []string{"currency", "sku"}, b.ColumnNames)
}

func TestFieldMappingPreservesAdvancedSQLByRefusingIt(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"SELECT * FROM input",
		"SELECT id FROM input WHERE id > 0",
		"SELECT id FROM input ORDER BY id",
		"SELECT id FROM input LIMIT 1",
		"SELECT DISTINCT id FROM input",
		"SELECT id FROM input AS x",
		"SELECT id FROM input UNION ALL SELECT id FROM other",
		"WITH a AS (SELECT id FROM input) SELECT id FROM a",
		"SELECT COUNT(id) AS count FROM input",
		"SELECT SUM(id) + 1 AS total FROM input",
		"SELECT ROW_NUMBER() OVER (ORDER BY id) AS n FROM input",
		"SELECT id AS duplicate, other AS duplicate FROM input",
		"SELECT id AS `nested.value` FROM input",
		"SELECT id /* keep this explanation */ FROM input",
		"SELECT id FROM input; SELECT id FROM input",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := ReadFieldMapping(query)
			require.Error(t, err)
		})
	}
}

func TestFieldMappingRejectsExpressionAndIdentifierInjection(t *testing.T) {
	t.Parallel()
	for _, mapping := range []FieldMapping{
		{Input: "input", Fields: []MappingField{{Name: "x", Expression: "id AS x, other"}}},
		{Input: "input", Fields: []MappingField{{Name: "x", Expression: "id FROM input WHERE id > 0 --"}}},
		{Input: "input", Fields: []MappingField{{Name: "x", Expression: "id; DELETE FROM input; SELECT id"}}},
		{Input: "input; DELETE", Fields: []MappingField{{Name: "x", Expression: "id"}}},
		{Input: "input", Fields: []MappingField{{Name: "x", Expression: "id"}, {Name: "x", Expression: "id"}}},
		{Input: "input", Fields: []MappingField{{Name: "", Expression: "id"}}},
		{Input: "input", Fields: []MappingField{{Name: "SKU", Expression: "id"}}},
		{Input: "input"},
	} {
		_, err := WriteFieldMapping(mapping)
		require.Error(t, err, "%+v", mapping)
	}
	query, err := WriteFieldMapping(FieldMapping{Input: "input", Fields: []MappingField{
		{Name: "a`b", Expression: "`source``name`"},
	}})
	require.NoError(t, err)
	require.Contains(t, query, "`a``b`")
}

func TestFieldMappingSuggestionsRequireCompatibleExactNames(t *testing.T) {
	t.Parallel()
	p18, p20, s2 := 18, 20, 2
	source := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{Name: "sku", Type: model.ContractFieldTypeValueString, Nullable: true},
		{Name: "count", Type: model.ContractFieldTypeValueInteger},
		{Name: "price", Type: model.ContractFieldTypeValueDecimal, Precision: &p20, Scale: &s2},
		{Name: "Name", Type: model.ContractFieldTypeValueString},
		{Name: "nested.value", Type: model.ContractFieldTypeValueString},
	}}
	destination := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{Name: "count", Type: model.ContractFieldTypeValueNumber},
		{Name: "sku", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "price", Type: model.ContractFieldTypeValueDecimal, Precision: &p18, Scale: &s2},
		{Name: "name", Type: model.ContractFieldTypeValueString},
		{Name: "Name", Type: model.ContractFieldTypeValueString},
		{Name: "currency", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "nested.value", Type: model.ContractFieldTypeValueString},
	}}
	mapping := SuggestFieldMapping("input", source, destination)
	require.Equal(t, []string{"count", "sku"}, mappingNames(&mapping))
	require.True(t, source.Fields[0].Nullable, "suggestions must not strengthen the source promise")
	require.True(t, destination.Fields[1].Required, "suggestions must not weaken the destination requirement")
	require.Empty(t, SuggestFieldMapping("input", nil, destination).Fields)
	require.Empty(t, SuggestFieldMapping("input", source, nil).Fields)
}

func TestFieldMappingRetainsQuotedConstantsAndRuntimeFailures(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		`'supplier #1 -- /* literal */'`, `'O''Brien'`, `'\\'`, `'żółć'`, `NULL`, `LOWER(TRIM(description))`,
	} {
		query, err := WriteFieldMapping(FieldMapping{Input: "input", Fields: []MappingField{{Name: "value", Expression: expression}}})
		require.NoError(t, err, expression)
		_, err = ReadFieldMapping(query)
		require.NoError(t, err)
		input := map[string]*pipepayload.Payload{"input": {
			Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"description": " Oak "}},
		}}
		before, err := NewRuntime().Execute(Transform{SQL: "SELECT " + expression + " AS value FROM input"}, input)
		require.NoError(t, err)
		after, err := NewRuntime().Execute(Transform{SQL: query}, input)
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	query, err := WriteFieldMapping(FieldMapping{Input: "input", Fields: []MappingField{{Name: "price", Expression: "CAST(price AS DECIMAL(18,2))"}}})
	require.NoError(t, err)
	_, err = NewRuntime().Execute(Transform{SQL: query}, map[string]*pipepayload.Payload{
		"input": {Representation: model.ContractRepresentationRows, Rows: []map[string]any{{"price": "ask for quote"}}},
	})
	require.Error(t, err)
	_, err = WriteFieldMapping(FieldMapping{Input: "input", Fields: []MappingField{{Name: "value", Expression: "id /* keep */"}}})
	require.Error(t, err)
}

func mappingNames(mapping *FieldMapping) []string {
	var names []string
	for _, field := range mapping.Fields {
		names = append(names, field.Name)
	}
	return names
}

func TestFieldMappingChecksSelectedOutputSerialization(t *testing.T) {
	t.Parallel()
	query := "SELECT JSON_OBJECT('sku', item_code) AS product FROM input"
	source := &pipepayload.Contract{Name: "supplier", Fields: []pipepayload.ContractField{{Name: "item_code", Type: model.ContractFieldTypeValueString}}}
	target := &pipepayload.Contract{Name: "catalog", Fields: []pipepayload.ContractField{
		{Name: "product", Type: model.ContractFieldTypeValueObject, Required: true},
		{Name: "product.sku", Type: model.ContractFieldTypeValueString, Required: true},
	}}
	require.NoError(t, ValidateFieldMappingContracts(query, model.TransformOutputKindJSON, source, target))
	require.Error(t, ValidateFieldMappingContracts(query, model.TransformOutputKindRows, source, target))
	target.Representation = model.ContractRepresentationRows
	require.Error(t, ValidateFieldMappingContracts(query, model.TransformOutputKindJSON, source, target))
	target.Representation, target.RootShape = model.ContractRepresentationJSON, model.ContractRootShapeObject
	require.Error(t, ValidateFieldMappingContracts(query, model.TransformOutputKindJSON, source, target))
	target.RootShape = model.ContractRootShapeArray
	require.NoError(t, ValidateFieldMappingContracts(query, model.TransformOutputKindJSON, source, target))
}
