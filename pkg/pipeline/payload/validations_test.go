package payload_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestCloneContractFieldValidations_NormalizesJSONNumberBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		wantMin  any
		wantMax  any
		wantEnum []any
	}{
		{
			name:     "NormalRangeIntegersAndDecimals",
			raw:      `{"min":0,"max":1000000,"enum":[1,2.5]}`,
			wantMin:  int64(0),
			wantMax:  int64(1000000),
			wantEnum: []any{int64(1), float64(2.5)},
		},
		{
			name:    "LargeExponentIntegerRemainsExact",
			raw:     `{"min":9007199254740993e0,"max":9223372036854775807.0}`,
			wantMin: json.Number("9007199254740993e0"),
			wantMax: json.Number("9223372036854775807.0"),
		},
		{
			name:     "ScientificNotation",
			raw:      `{"min":1e6,"max":2.5e-3,"enum":[1e2,2.5e-3]}`,
			wantMin:  float64(1000000),
			wantMax:  float64(0.0025),
			wantEnum: []any{float64(100), float64(0.0025)},
		},
		{
			name:     "NegativeBounds",
			raw:      `{"min":-100,"max":-1,"enum":[-2,-3.5]}`,
			wantMin:  int64(-100),
			wantMax:  int64(-1),
			wantEnum: []any{int64(-2), float64(-3.5)},
		},
		{
			name:    "NestedEnumStructures",
			raw:     `{"enum":[[1,2.5],{"min":1,"ratio":2.5e-3}]}`,
			wantMin: nil,
			wantMax: nil,
			wantEnum: []any{
				[]any{int64(1), float64(2.5)},
				map[string]any{"min": int64(1), "ratio": float64(0.0025)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var validations payload.ContractFieldValidations
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &validations))

			cloned := payload.CloneContractFieldValidations(&validations)

			if tt.wantMin == nil {
				require.Nil(t, cloned.Min)
			} else {
				require.IsType(t, tt.wantMin, cloned.Min)
			}
			require.Equal(t, tt.wantMin, cloned.Min)
			if tt.wantMax == nil {
				require.Nil(t, cloned.Max)
			} else {
				require.IsType(t, tt.wantMax, cloned.Max)
			}
			require.Equal(t, tt.wantMax, cloned.Max)
			require.Equal(t, tt.wantEnum, cloned.Enum)
		})
	}
}

func TestValidateContractFieldValues_UniquePreservesLargeIntegerPrecision(t *testing.T) {
	t.Parallel()

	unique := true
	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "id",
			Type: model.ContractFieldTypeValueInteger,
			Validations: &payload.ContractFieldValidations{
				Unique: &unique,
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"id": int64(9007199254740992)},
		{"id": int64(9007199254740993)},
	})

	require.Nil(t, failure)
}

func TestValidateContractFieldValues_UniqueAcceptsTimeValues(t *testing.T) {
	t.Parallel()

	unique := true
	contract := &payload.Contract{
		Name:           "events",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "created_at",
			Type: model.ContractFieldTypeValueDatetime,
			Validations: &payload.ContractFieldValidations{
				Unique: &unique,
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"created_at": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"created_at": time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)},
	})

	require.Nil(t, failure)
}

func TestContractHasFieldValidations_TreatsDisabledUniqueAsInactive(t *testing.T) {
	t.Parallel()

	unique := false
	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "id",
			Type: model.ContractFieldTypeValueInteger,
			Validations: &payload.ContractFieldValidations{
				Unique: &unique,
			},
		}},
	}

	require.False(t, payload.ContractHasFieldValidations(contract))
}

func TestValidateContractFieldValues_EnforcesRequiredAndNonNullableWithoutRules(t *testing.T) {
	t.Parallel()

	disabledUnique := false
	activeUnique := true
	for _, tc := range []struct {
		name     string
		field    payload.ContractField
		rows     []map[string]any
		wantRule payload.ContractValidationRule
	}{
		{"empty validations", payload.ContractField{Name: "id", Validations: &payload.ContractFieldValidations{}}, []map[string]any{{"id": nil}}, payload.ContractValidationRuleUnknown},
		{"disabled unique", payload.ContractField{Name: "id", Validations: &payload.ContractFieldValidations{Unique: &disabledUnique}}, []map[string]any{{"id": nil}}, payload.ContractValidationRuleUnknown},
		{"unique only", payload.ContractField{Name: "id", Validations: &payload.ContractFieldValidations{Unique: &activeUnique}}, []map[string]any{{"id": nil}}, payload.ContractValidationRuleUnique},
		{"required null", payload.ContractField{Name: "id", Required: true}, []map[string]any{{"id": nil}}, payload.ContractValidationRuleUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := payload.ValidateContractFieldValues(&payload.Contract{Fields: []payload.ContractField{tc.field}}, tc.rows)
			require.NotNil(t, failure)
			require.Equal(t, tc.wantRule, failure.Rule)
		})
	}

	optional := &payload.Contract{Fields: []payload.ContractField{{Name: "items[].sku"}}}
	require.Nil(t, payload.ValidateContractFieldValues(optional, []map[string]any{{
		"items": []any{map[string]any{"sku": "one"}, map[string]any{}},
	}}))
	nullable := &payload.Contract{Fields: []payload.ContractField{{Name: "id", Nullable: true}}}
	require.Nil(t, payload.ValidateContractFieldValues(nullable, []map[string]any{{"id": nil}}))
	require.Nil(t, payload.ValidateContractFieldValues(optional, nil))
}

func TestValidateContractFieldValues_LengthAcceptsTypedSlices(t *testing.T) {
	t.Parallel()

	minLength := 1
	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "tags",
			Type: model.ContractFieldTypeValueArray,
			Validations: &payload.ContractFieldValidations{
				MinLength: &minLength,
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"tags": []string{"vip"}},
		{"tags": []int64{42}},
	})

	require.Nil(t, failure)
}

func TestValidateContractFieldValues_NonNullableNullFailsValidation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "status",
			Type: model.ContractFieldTypeValueString,
			Validations: &payload.ContractFieldValidations{
				Enum: []any{"active"},
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"status": nil},
	})

	require.NotNil(t, failure)
	require.Equal(t, payload.ContractValidationRuleEnum, failure.Rule)
	require.Equal(t, 1, failure.ViolationCount)
}

func TestValidateContractFieldValues_NullableNullSkipsValidation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name:     "status",
			Type:     model.ContractFieldTypeValueString,
			Nullable: true,
			Validations: &payload.ContractFieldValidations{
				Enum: []any{"active"},
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"status": nil},
	})

	require.Nil(t, failure)
}

func TestValidateContractFieldValues_RequiredValidatedFieldMissingInRowFailsValidation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name:     "status",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
			Validations: &payload.ContractFieldValidations{
				Enum: []any{"active"},
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"status": "active"},
		{"email": "missing-status@example.com"},
	})

	require.NotNil(t, failure)
	require.Equal(t, payload.ContractValidationRuleEnum, failure.Rule)
	require.Equal(t, 1, failure.ViolationCount)
}

func TestValidateContractFieldValues_FlatFieldNameWithPathPunctuation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name:     "profile.email",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
			Validations: &payload.ContractFieldValidations{
				Format: "email",
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"profile.email": "user@example.com"},
	})

	require.Nil(t, failure)
}

func TestValidateContractFieldValues_FormatAcceptsUUIDv7(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "id",
			Type: model.ContractFieldTypeValueString,
			Validations: &payload.ContractFieldValidations{
				Format: "uuid",
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"id": "01890f9e-7cc2-7a93-9a22-66e5db35d67a"},
	})

	require.Nil(t, failure)
}

func TestValidateContractFieldValues_NumberValidationRejectsUncoercedDecimalStrings(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "invoices",
		Representation: model.ContractRepresentationRows,
		Fields: []payload.ContractField{{
			Name: "amount",
			Type: model.ContractFieldTypeValueNumber,
			Validations: &payload.ContractFieldValidations{
				Min: float64(1),
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"amount": "2.50"},
	})

	require.NotNil(t, failure)
	require.Equal(t, payload.ContractValidationRuleMin, failure.Rule)
}

func TestCoerceContractFieldValidationRows_NumberValidationCoercesSerializedDecimalStrings(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "invoices",
		Representation: model.ContractRepresentationCSV,
		Fields: []payload.ContractField{{
			Name: "amount",
			Type: model.ContractFieldTypeValueNumber,
			Validations: &payload.ContractFieldValidations{
				Min: float64(1),
			},
		}},
	}

	rows := payload.CoerceContractFieldValidationRows(contract, []map[string]any{
		{"amount": "2.50"},
	})

	failure := payload.ValidateContractFieldValues(contract, rows)
	require.Nil(t, failure)
}

func TestValidateContractFieldValues_PathResolutionErrorFailsValidation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "users",
		Representation: model.ContractRepresentationJSON,
		Fields: []payload.ContractField{{
			Name: "profile.email",
			Type: model.ContractFieldTypeValueString,
			Validations: &payload.ContractFieldValidations{
				Format: "email",
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{
		{"profile": "wrong-shape"},
	})
	require.NotNil(t, failure)
	require.Equal(t, payload.ContractValidationRuleFormat, failure.Rule)
	require.Equal(t, 1, failure.ViolationCount)
}

func TestValidateContractFieldValues_PartiallyMissingArrayPathFailsValidation(t *testing.T) {
	t.Parallel()

	contract := &payload.Contract{
		Name:           "orders",
		Representation: model.ContractRepresentationJSON,
		Fields: []payload.ContractField{{
			Name: "items[].sku",
			Type: model.ContractFieldTypeValueString,
			Validations: &payload.ContractFieldValidations{
				Format: "uuid",
			},
		}},
	}

	failure := payload.ValidateContractFieldValues(contract, []map[string]any{{
		"items": []any{
			map[string]any{"sku": "01890f9e-7cc2-7a93-9a22-66e5db35d67a"},
			map[string]any{"name": "missing-sku"},
		},
	}})
	require.NotNil(t, failure)
	require.Equal(t, payload.ContractValidationRuleFormat, failure.Rule)
	require.Equal(t, 1, failure.ViolationCount)
}

func TestIntegerValidationRejectsUnsupportedRanges(t *testing.T) {
	t.Parallel()
	contract := &payload.Contract{Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueInteger, Validations: &payload.ContractFieldValidations{Min: int64(-9223372036854775808)}}}}
	require.Nil(t, payload.ValidateContractFieldValues(contract, []map[string]any{{"id": json.Number("9223372036854775807")}}))
	for _, value := range []any{float64(0x1p63), float32(0x1p63), json.Number("9223372036854775808")} {
		require.NotNil(t, payload.ValidateContractFieldValues(contract, []map[string]any{{"id": value}}))
	}
	bound := json.Number("9223372036854775809")
	cloned := payload.CloneContractFieldValidations(&payload.ContractFieldValidations{Max: bound})
	require.Equal(t, bound, cloned.Max)
	contract.Fields[0].Validations = cloned
	require.Error(t, payload.ValidateContractFieldDeclarations(contract))
}

func TestNumberValidationRejectsLossyIntegerConversion(t *testing.T) {
	t.Parallel()
	contract := &payload.Contract{Fields: []payload.ContractField{{Name: "id", Type: model.ContractFieldTypeValueNumber, Validations: &payload.ContractFieldValidations{Min: json.Number("9007199254740996")}}}}
	require.NoError(t, payload.ValidateContractFieldDeclarations(contract))
	for _, value := range []any{int64(9007199254740995), json.Number("9007199254740995"), json.Number("9007199254740995e0")} {
		require.NotNil(t, payload.ValidateContractFieldValues(contract, []map[string]any{{"id": value}}))
	}
	rows := payload.CoerceContractFieldValidationRows(contract, []map[string]any{{"id": "9007199254740995"}})
	require.Equal(t, "9007199254740995", rows[0]["id"])
	require.NotNil(t, payload.ValidateContractFieldValues(contract, rows))
	for _, value := range []any{int64(9007199254740996), json.Number("9007199254740996e0"), uint64(1 << 63)} {
		require.Nil(t, payload.ValidateContractFieldValues(contract, []map[string]any{{"id": value}}))
	}
	contract.Fields[0].Validations.Min = json.Number("9007199254740995")
	require.ErrorContains(t, payload.ValidateContractFieldDeclarations(contract), "lossless integer conversion")
	contract.Fields[0].Validations = &payload.ContractFieldValidations{Max: int64(0)}
	require.NotNil(t, payload.ValidateContractFieldValues(contract, []map[string]any{{"id": int64(-9007199254740993)}}))
}
