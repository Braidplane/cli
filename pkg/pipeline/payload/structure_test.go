package payload

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/stretchr/testify/require"
)

func structure(root *StructuralType) *Structure {
	return &Structure{
		Contract: "agreement", Object: "dataset", Revision: "r1",
		Representation: model.ContractRepresentationJSON, RootShape: model.ContractRootShapeObject, Root: root,
	}
}

func object(properties ...StructuralProperty) *StructuralType {
	return &StructuralType{Kind: StructuralObject, Additional: StructuralClosed, Properties: properties}
}

func property(name string, required bool, kind StructuralKind) StructuralProperty {
	return StructuralProperty{Name: name, Required: required, Type: &StructuralType{Kind: kind}}
}

func exact(t *testing.T, token string) *decimal.Value {
	t.Helper()
	v, err := decimal.Parse(token)
	require.NoError(t, err)
	return &v
}

func structuralCode(t *testing.T, err error, code StructuralCode) {
	t.Helper()
	var failure *StructuralError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, code, failure.Code)
	require.Equal(t, code.String(), err.Error())
}

func TestStructuralPresenceAndLiteralPaths(t *testing.T) {
	parent := StructuralProperty{Name: "parent", Type: object(property("child", true, StructuralString))}
	s := structure(object(parent, property("a.b", true, StructuralBoolean)))
	for _, tc := range []struct {
		name  string
		value map[string]any
		code  StructuralCode
	}{
		{"absent optional parent", map[string]any{"a.b": true}, StructuralCodeUnknown},
		{"present parent", map[string]any{"parent": map[string]any{"child": "ok"}, "a.b": true}, StructuralCodeUnknown},
		{"missing child", map[string]any{"parent": map[string]any{}, "a.b": true}, StructurePresence},
		{"null parent", map[string]any{"parent": nil, "a.b": true}, StructureNull},
		{"null child", map[string]any{"parent": map[string]any{"child": nil}, "a.b": true}, StructureNull},
		{"nested is not literal", map[string]any{"a": map[string]any{"b": true}}, StructurePresence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStructuralValue(s, tc.value, DefaultStructuralLimits())
			if tc.code == StructuralCodeUnknown {
				require.NoError(t, err)
			} else {
				structuralCode(t, err, tc.code)
			}
		})
	}
	parent.Type.Nullable = true
	s.Root.Properties[0] = parent
	require.NoError(t, ValidateStructuralValue(s, map[string]any{"parent": nil, "a.b": true}, DefaultStructuralLimits()))
	s.Root.Properties[0].Required = true
	structuralCode(t, ValidateStructuralValue(s, map[string]any{"a.b": true}, DefaultStructuralLimits()), StructurePresence)
	literal := StructuralPath{{Kind: StructuralPropertySegment, Name: "a.b"}}
	nested := StructuralPath{{Kind: StructuralPropertySegment, Name: "a"}, {Kind: StructuralPropertySegment, Name: "b"}}
	require.NotEqual(t, literal.String(), nested.String())
	require.Equal(t, `$["a.b"]`, literal.String())
}

func TestStructuralCollections(t *testing.T) {
	minimum, maximum := 1, 2
	for _, tc := range []struct {
		name      string
		typeOf    *StructuralType
		good, bad any
	}{
		{
			"array branches", &StructuralType{Kind: StructuralArray, Items: object(property("id", true, StructuralString))},
			[]any{map[string]any{"id": "x"}, map[string]any{"id": "y"}},
			[]any{map[string]any{"id": "x"}, map[string]any{}},
		},
		{
			"array lengths", &StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralString}, MinLength: &minimum, MaxLength: &maximum},
			[]any{"a", "b"},
			[]any{},
		},
		{
			"nullable items", &StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralString, Nullable: true}},
			[]any{nil},
			[]any{true},
		},
		{
			"map keys", &StructuralType{Kind: StructuralMap, Keys: &StructuralType{Kind: StructuralString, Enum: []any{"a.b"}}, Values: &StructuralType{Kind: StructuralBoolean}},
			map[string]any{"a.b": true},
			map[string]any{"a": true},
		},
		{
			"map values", &StructuralType{Kind: StructuralMap, Keys: &StructuralType{Kind: StructuralString}, Values: &StructuralType{Kind: StructuralString}},
			map[string]any{"free": "text"},
			map[string]any{"free": false},
		},
		{
			"vector dimension", &StructuralType{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}},
			[]any{float32(0.1), float32(2)},
			[]any{float32(1)},
		},
		{
			"vector precision", &StructuralType{Kind: StructuralVector, Dimension: 1, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}},
			[]any{float32(0.1)},
			[]any{float64(0.1)},
		},
		{
			"unique observed values", &StructuralType{Kind: StructuralArray, Unique: true, Items: object(property("id", true, StructuralString))},
			[]any{map[string]any{"id": "a"}, map[string]any{"id": "b"}},
			[]any{map[string]any{"id": "a"}, map[string]any{"id": "a"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := structure(tc.typeOf)
			require.NoError(t, ValidateStructuralValue(s, tc.good, DefaultStructuralLimits()))
			require.Error(t, ValidateStructuralValue(s, tc.bad, DefaultStructuralLimits()))
		})
	}
	require.NoError(t, ValidateStructuralValue(structure(&StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralString}}), []any{}, DefaultStructuralLimits()))
}

func TestStructuralExactValuesAndTemporalSemantics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		typeOf    *StructuralType
		good, bad any
	}{
		{"decimal digits", &StructuralType{Kind: StructuralDecimal, Precision: 20, Scale: 2}, json.Number("9007199254740993.01"), json.Number("1000000000000000000.01")},
		{"decimal scale", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, json.Number("1.20"), json.Number("1.201")},
		{"decimal no text coercion", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, json.Number("1.25"), "1.25"},
		{"decimal no float coercion", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, json.Number("1.25"), float64(1.25)},
		{"signed integer range", &StructuralType{Kind: StructuralInteger, Bits: 8}, int64(-128), int64(128)},
		{"integer no float repair", &StructuralType{Kind: StructuralInteger, Bits: 64}, json.Number("9007199254740993"), float64(1)},
		{"exact bounds", &StructuralType{Kind: StructuralDecimal, Precision: 20, Scale: 2, Min: exact(t, "9007199254740993.01"), Max: exact(t, "9007199254740993.02")}, json.Number("9007199254740993.02"), json.Number("9007199254740993.03")},
		{"date", &StructuralType{Kind: StructuralDate}, "2024-02-29", "2025-02-29"},
		{"local time", &StructuralType{Kind: StructuralLocalTime}, "12:30:00.123", "12:30:00Z"},
		{"local datetime", &StructuralType{Kind: StructuralLocalDatetime}, "2026-10-17T12:30:00", "2026-10-17T12:30:00Z"},
		{"instant", &StructuralType{Kind: StructuralInstant}, "2026-10-17T12:30:00+02:00", "2026-10-17T12:30:00"},
		{"strict offset", &StructuralType{Kind: StructuralInstant}, "2026-10-17T12:30:00Z", "2026-10-17T12:30:00+24:00"},
		{"strict local clock", &StructuralType{Kind: StructuralLocalTime}, "01:30:00", "1:30:00"},
		{"no time truncation", &StructuralType{Kind: StructuralInstant}, "2026-10-17T12:30:00.123456789Z", "2026-10-17T12:30:00.1234567891Z"},
		{"string enum", &StructuralType{Kind: StructuralString, Enum: []any{"allowed"}}, "allowed", "other"},
		{"format", &StructuralType{Kind: StructuralString, Format: "uuid"}, "f8655bc4-f3c6-4551-adeb-c9f2fb80d45d", "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := structure(tc.typeOf)
			require.NoError(t, ValidateStructuralValue(s, tc.good, DefaultStructuralLimits()))
			require.Error(t, ValidateStructuralValue(s, tc.bad, DefaultStructuralLimits()))
		})
	}
}

func TestStructuralRecursiveCompatibility(t *testing.T) {
	one, two, three := 1, 2, 3
	text := &StructuralType{Kind: StructuralString}
	for _, tc := range []struct {
		name       string
		p, c       *StructuralType
		compatible bool
	}{
		{"array length subset", &StructuralType{Kind: StructuralArray, Items: text, MinLength: &two, MaxLength: &two}, &StructuralType{Kind: StructuralArray, Items: text, MinLength: &one, MaxLength: &three}, true},
		{"array unbounded length", &StructuralType{Kind: StructuralArray, Items: text}, &StructuralType{Kind: StructuralArray, Items: text, MaxLength: &two}, false},
		{"array item nullability", &StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralString, Nullable: true}}, &StructuralType{Kind: StructuralArray, Items: text}, false},
		{"array item nested break", &StructuralType{Kind: StructuralArray, Items: object(property("x", false, StructuralString))}, &StructuralType{Kind: StructuralArray, Items: object(property("x", true, StructuralString))}, false},
		{"map widening", &StructuralType{Kind: StructuralMap, Keys: text, Values: &StructuralType{Kind: StructuralInteger, Bits: 16}}, &StructuralType{Kind: StructuralMap, Keys: text, Values: &StructuralType{Kind: StructuralInteger, Bits: 32}}, true},
		{"map constrained keys", &StructuralType{Kind: StructuralMap, Keys: text, Values: text}, &StructuralType{Kind: StructuralMap, Keys: &StructuralType{Kind: StructuralString, Enum: []any{"key"}}, Values: text}, false},
		{"map constrained values", &StructuralType{Kind: StructuralMap, Keys: text, Values: &StructuralType{Kind: StructuralBoolean}}, &StructuralType{Kind: StructuralMap, Keys: text, Values: text}, false},
		{"vector element widening", &StructuralType{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}}, &StructuralType{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralNumber, Bits: 64}}, true},
		{"vector dimension break", &StructuralType{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}}, &StructuralType{Kind: StructuralVector, Dimension: 3, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}}, false},
		{"vector not array", &StructuralType{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}}, &StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralNumber, Bits: 32}}, false},
		{"uniqueness guarantee", &StructuralType{Kind: StructuralArray, Items: text, Unique: true}, &StructuralType{Kind: StructuralArray, Items: text}, true},
		{"uniqueness not inferred", &StructuralType{Kind: StructuralArray, Items: text}, &StructuralType{Kind: StructuralArray, Items: text, Unique: true}, false},
		{"format guarantee", &StructuralType{Kind: StructuralString, Format: "uuid"}, &StructuralType{Kind: StructuralString, Format: "uuid"}, true},
		{"format not inferred", text, &StructuralType{Kind: StructuralString, Format: "uuid"}, false},
		{"exact bound narrowing", &StructuralType{Kind: StructuralDecimal, Precision: 20, Scale: 2, Max: exact(t, "9007199254740993.02")}, &StructuralType{Kind: StructuralDecimal, Precision: 20, Scale: 2, Max: exact(t, "9007199254740993.01")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckStructuralCompatibility(structure(tc.p), structure(tc.c), DefaultStructuralLimits())
			if tc.compatible {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	p, c := structure(text), structure(text)
	c.Representation, c.RootShape = model.ContractRepresentationRows, model.ContractRootShapeUnknown
	structuralCode(t, CheckStructuralCompatibility(p, c, DefaultStructuralLimits()), StructureEncoding)
}

func TestStructuralCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name       string
		p, c       *StructuralType
		compatible bool
	}{
		{"integer widening", &StructuralType{Kind: StructuralInteger, Bits: 16}, &StructuralType{Kind: StructuralInteger, Bits: 32}, true},
		{"integer narrowing", &StructuralType{Kind: StructuralInteger, Bits: 64}, &StructuralType{Kind: StructuralInteger, Bits: 32}, false},
		{"integer decimal range", &StructuralType{Kind: StructuralInteger, Bits: 64}, &StructuralType{Kind: StructuralDecimal, Precision: 19}, true},
		{"integer decimal overflow", &StructuralType{Kind: StructuralInteger, Bits: 64}, &StructuralType{Kind: StructuralDecimal, Precision: 18}, false},
		{"integer approximate exact", &StructuralType{Kind: StructuralInteger, Bits: 32}, &StructuralType{Kind: StructuralNumber, Bits: 64}, true},
		{"integer approximate lossy", &StructuralType{Kind: StructuralInteger, Bits: 64}, &StructuralType{Kind: StructuralNumber, Bits: 64}, false},
		{"bounded integer widening", &StructuralType{Kind: StructuralInteger, Bits: 64, Min: exact(t, "-10"), Max: exact(t, "10")}, &StructuralType{Kind: StructuralNumber, Bits: 32}, true},
		{"decimal widening", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, &StructuralType{Kind: StructuralDecimal, Precision: 6, Scale: 3}, true},
		{"decimal integer digits", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, &StructuralType{Kind: StructuralDecimal, Precision: 6, Scale: 4}, false},
		{"decimal scale loss", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, &StructuralType{Kind: StructuralDecimal, Precision: 6, Scale: 1}, false},
		{"decimal float forbidden", &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}, &StructuralType{Kind: StructuralNumber, Bits: 64}, false},
		{"float decimal forbidden", &StructuralType{Kind: StructuralNumber, Bits: 32}, &StructuralType{Kind: StructuralDecimal, Precision: 38, Scale: 20}, false},
		{"float widening", &StructuralType{Kind: StructuralNumber, Bits: 32}, &StructuralType{Kind: StructuralNumber, Bits: 64}, true},
		{"local instant distinct", &StructuralType{Kind: StructuralLocalDatetime}, &StructuralType{Kind: StructuralInstant}, false},
		{"optional not guaranteed", object(property("x", false, StructuralString)), object(property("x", true, StructuralString)), false},
		{"optional parent required child", object(StructuralProperty{Name: "p", Type: object(property("x", true, StructuralString))}), object(StructuralProperty{Name: "p", Type: object(property("x", true, StructuralString))}), true},
		{"optional field incompatible when present", object(property("x", false, StructuralBoolean)), object(property("x", false, StructuralString)), false},
		{"missing optional field", object(), object(property("x", false, StructuralString)), true},
		{"unknown additional optional", &StructuralType{Kind: StructuralObject, Additional: StructuralOpen}, object(property("x", false, StructuralString)), false},
		{"closed consumer extras", object(property("x", false, StructuralString)), object(), false},
		{"open consumer extras", object(property("x", false, StructuralString)), &StructuralType{Kind: StructuralObject, Additional: StructuralOpen}, true},
		{"nullability", &StructuralType{Kind: StructuralString, Nullable: true}, &StructuralType{Kind: StructuralString}, false},
		{"enum subset", &StructuralType{Kind: StructuralString, Enum: []any{"a"}}, &StructuralType{Kind: StructuralString, Enum: []any{"a", "b"}}, true},
		{"enum superset", &StructuralType{Kind: StructuralString, Enum: []any{"a", "b"}}, &StructuralType{Kind: StructuralString, Enum: []any{"a"}}, false},
		{"unknown enum", &StructuralType{Kind: StructuralString}, &StructuralType{Kind: StructuralString, Enum: []any{"a"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckStructuralCompatibility(structure(tc.p), structure(tc.c), DefaultStructuralLimits())
			if tc.compatible {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestStructuralIdentityAndPathBreaks(t *testing.T) {
	before := structure(object(StructuralProperty{Name: "old", ID: "stable", Required: true, Type: &StructuralType{Kind: StructuralString}}, property("unidentified", false, StructuralString)))
	after := structure(object(StructuralProperty{Name: "new", ID: "stable", Required: true, Type: &StructuralType{Kind: StructuralString}}, property("renamed", false, StructuralString)))
	after.Revision = "r2"
	matches, err := MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 3)
	require.True(t, matches[0].PathChanged)
	require.Equal(t, "stable", matches[0].After.ID)
	require.NotEqual(t, matches[0].Before.Identity, matches[0].After.Identity)
	require.Nil(t, matches[1].After)
	require.Nil(t, matches[2].Before)
	structuralCode(t, CheckStructuralCompatibility(after, before, DefaultStructuralLimits()), StructurePresence)

	// Reordering is not identity; fallback includes the exact revision and path.
	before = structure(object(property("a.b", false, StructuralString), StructuralProperty{Name: "a", Type: object(property("b", false, StructuralString))}))
	after = structure(object(before.Root.Properties[1], before.Root.Properties[0]))
	after.Revision = "r2"
	matches, err = MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 3)
	for _, match := range matches {
		require.NotNil(t, match.After)
		require.False(t, match.PathChanged)
		require.NotEqual(t, match.Before.Identity, match.After.Identity)
	}
	// The same authored ID may occur under distinct parents, but cannot migrate.
	child := StructuralProperty{Name: "value", ID: "shared", Type: &StructuralType{Kind: StructuralString}}
	before = structure(object(StructuralProperty{Name: "a", ID: "p1", Type: object(child)}, StructuralProperty{Name: "b", ID: "p2", Type: object()}))
	after = structure(object(StructuralProperty{Name: "a", ID: "p1", Type: object()}, StructuralProperty{Name: "b", ID: "p2", Type: object(child)}))
	matches, err = MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 4)
	require.Nil(t, matches[1].After)
	require.Nil(t, matches[3].Before)
	before.Root.Properties[1].Type = object(child)
	require.NoError(t, ValidateStructure(before, DefaultStructuralLimits()))
	before.Root.Properties[0].Type.Properties = append(before.Root.Properties[0].Type.Properties, child)
	matches, err = MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.Nil(t, matches)
	structuralCode(t, err, StructureAmbiguous)
}

func TestStructuralLimitsAndSafeFailures(t *testing.T) {
	s := structure(object(property("private.name", true, StructuralString)))
	for _, tc := range []struct {
		name   string
		limits StructuralLimits
	}{
		{"depth", StructuralLimits{Depth: 1, Nodes: 4096, Bytes: 1 << 20, Work: 1_000_000}},
		{"nodes", StructuralLimits{Depth: 32, Nodes: 1, Bytes: 1 << 20, Work: 1_000_000}},
		{"bytes", StructuralLimits{Depth: 32, Nodes: 4096, Bytes: 8, Work: 1_000_000}},
		{"work", StructuralLimits{Depth: 32, Nodes: 4096, Bytes: 1 << 20, Work: 1}},
		{"invalid ceiling", StructuralLimits{Depth: 33, Nodes: 4096, Bytes: 1 << 20, Work: 1_000_000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			structuralCode(t, ValidateStructure(s, tc.limits), StructureLimit)
			matches, err := MatchStructuralDeclarations(s, s, tc.limits)
			require.Nil(t, matches)
			structuralCode(t, err, StructureLimit)
		})
	}
	structuralCode(t, ValidateStructuralValue(s, map[string]any{"private.name": strings.Repeat("secret", 200_000)}, DefaultStructuralLimits()), StructureLimit)
	err := ValidateStructuralValue(s, map[string]any{"private.name": false}, DefaultStructuralLimits())
	structuralCode(t, err, StructureType)
	var failure *StructuralError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, StructuralLocator{0}, failure.Locator)
	encoded, err := json.Marshal(failure)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	cycle := object()
	cycle.Properties = []StructuralProperty{{Name: "cycle", Type: cycle}}
	structuralCode(t, ValidateStructure(structure(cycle), DefaultStructuralLimits()), StructureInvalid)
	valueCycle := map[string]any{}
	valueCycle["self"] = valueCycle
	open := structure(&StructuralType{Kind: StructuralObject, Additional: StructuralOpen})
	structuralCode(t, ValidateStructuralValue(open, valueCycle, DefaultStructuralLimits()), StructureLimit)
	structuralCode(t, ValidateStructuralValue(open, map[string]any{"ignored": math.NaN()}, DefaultStructuralLimits()), StructureUnsupported)
	for _, invalid := range []*StructuralType{
		{Kind: StructuralKind(-1)},
		{Kind: StructuralObject},
		{Kind: StructuralVector, Dimension: 2, Items: &StructuralType{Kind: StructuralString}},
		{Kind: StructuralArray},
		{Kind: StructuralString, Precision: 4},
		{Kind: StructuralString, Format: "unknown"},
		{Kind: StructuralInteger, Bits: 8, Min: exact(t, "200")},
	} {
		require.Error(t, ValidateStructure(structure(invalid), DefaultStructuralLimits()))
	}
}

func TestStructuralScopedContinuity(t *testing.T) {
	child := StructuralProperty{Name: "child", ID: "child-id", Type: &StructuralType{Kind: StructuralString}}
	before := structure(object(StructuralProperty{Name: "old", ID: "parent-id", Type: object(child)}))
	after := structure(object(StructuralProperty{Name: "new", ID: "parent-id", Type: object(child)}))
	matches, err := MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 2)
	for _, match := range matches {
		require.NotNil(t, match.After)
		require.True(t, match.PathChanged)
	}
	after.Object = "another-object"
	matches, err = MatchStructuralDeclarations(before, after, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 4)
	for _, match := range matches {
		require.True(t, match.Before == nil || match.After == nil)
	}
	// Distinct sibling names cannot disambiguate duplicate authored IDs.
	s := structure(object(child, StructuralProperty{Name: "different", ID: child.ID, Type: child.Type}))
	structuralCode(t, ValidateStructure(s, DefaultStructuralLimits()), StructureAmbiguous)
	// Collection ancestry is structural, never a literal key or item index.
	before = structure(object(StructuralProperty{Name: "items", Type: &StructuralType{Kind: StructuralArray, Items: object(child)}}, StructuralProperty{Name: "map", Type: &StructuralType{Kind: StructuralMap, Keys: &StructuralType{Kind: StructuralString}, Values: object(child)}}))
	matches, err = MatchStructuralDeclarations(before, before, DefaultStructuralLimits())
	require.NoError(t, err)
	require.Len(t, matches, 4)
	require.Contains(t, matches[1].Before.Path.String(), "[*]")
	require.Contains(t, matches[3].Before.Path.String(), "{value}")
	require.NotEqual(t, matches[1].Before.Identity, matches[3].Before.Identity)
}

func TestStructuralValueBudgetsAndNoRepair(t *testing.T) {
	limits := DefaultStructuralLimits()
	limits.Work = 1200
	array := structure(&StructuralType{Kind: StructuralArray, Items: &StructuralType{Kind: StructuralInteger, Bits: 16}, Unique: true})
	values := make([]any, 100)
	for i := range values {
		values[i] = i
	}
	structuralCode(t, ValidateStructuralValue(array, values, limits), StructureLimit)
	limits = DefaultStructuralLimits()
	limits.Nodes = 10
	structuralCode(t, ValidateStructuralValue(array, values, limits), StructureLimit)
	input := map[string]any{"amount": json.Number("1.250"), "untouched": []any{"value"}}
	s := structure(object(StructuralProperty{Name: "amount", Required: true, Type: &StructuralType{Kind: StructuralDecimal, Precision: 5, Scale: 2}}))
	s.Root.Additional = StructuralOpen
	require.NoError(t, ValidateStructuralValue(s, input, DefaultStructuralLimits()))
	require.Equal(t, json.Number("1.250"), input["amount"])
	require.Equal(t, []any{"value"}, input["untouched"])
	// A mismatch cannot skip a large unconstrained branch during preflight.
	input["amount"], input["untouched"] = false, strings.Repeat("x", 1<<20)
	structuralCode(t, ValidateStructuralValue(s, input, DefaultStructuralLimits()), StructureLimit)
}

func TestStructuralMapDiagnosticsAreDeterministic(t *testing.T) {
	s := structure(&StructuralType{
		Kind:   StructuralMap,
		Keys:   &StructuralType{Kind: StructuralString, Enum: []any{"valid"}},
		Values: &StructuralType{Kind: StructuralString},
	})
	input := map[string]any{"bad-key": "text", "valid": false}
	for range 200 {
		err := ValidateStructuralValue(s, input, DefaultStructuralLimits())
		structuralCode(t, err, StructureConstraint)
		var failure *StructuralError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, StructuralLocator{-2}, failure.Locator)
	}
	open := structure(&StructuralType{Kind: StructuralObject, Additional: StructuralOpen})
	input = map[string]any{"invalid": "\xff", "large": strings.Repeat("x", 1000)}
	limits := DefaultStructuralLimits()
	limits.Work = 500
	for range 200 {
		structuralCode(t, ValidateStructuralValue(open, input, limits), StructureInvalid)
	}
	limits.Work = 200
	for range 200 {
		structuralCode(t, ValidateStructuralValue(s, map[string]any{"valid": "text", "another-key": "text"}, limits), StructureLimit)
	}
}
