package sql

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

func executeTransformStepFromPayload(
	transform *Transform,
	upstreamPayload *pipepayload.Payload,
) (*pipepayload.Output, error) {
	return executeTransformStepFromPayloads(transform, map[string]*pipepayload.Payload{
		"input": upstreamPayload,
	})
}

func runtimeRowsPayload(
	columnNames []string,
	rowCount int,
	rows []map[string]any,
) *pipepayload.Payload {
	return &pipepayload.Payload{
		Representation: model.ContractRepresentationRows,
		ColumnNames:    append([]string(nil), columnNames...),
		RowCount:       rowCount,
		Rows:           rows,
	}
}

func TestValidateTransformSQL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		assertErr func(t *testing.T, err error)
	}{
		{
			name: "AcceptsSimpleSelect",
			sql:  "SELECT id, email FROM input",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsSelectWithCast",
			sql:  "SELECT id, CAST(total AS SIGNED) AS total_int FROM input",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsSelectWithWhereOrderAndLimit",
			sql:  "SELECT id FROM input WHERE is_active = true ORDER BY id LIMIT 100",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsCSVIndexShorthandProjection",
			sql:  "SELECT csv_row ->[0] AS first_col FROM input",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsCSVIndexShorthandInWhere",
			sql:  "SELECT id FROM input WHERE csv_row ->[0] = 'alice'",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsCountOneAggregate",
			sql:  "SELECT COUNT(1) AS total FROM input",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "AcceptsGroupByWithGroupedProjectionAndAggregate",
			sql:  "SELECT category AS category, COUNT(*) AS total FROM input GROUP BY category",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsGroupByWhenProjectionColumnIsNotGrouped",
			sql:  "SELECT category AS category, id AS id FROM input GROUP BY category",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, "must appear in GROUP BY")
			},
		},
		{
			name: "RejectsGroupByDerivedExpression",
			sql:  "SELECT category AS category, COUNT(*) AS total FROM input GROUP BY CAST(category AS CHAR)",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, "group by supports only column references")
			},
		},
		{
			name: "RejectsNonSelectStatement",
			sql:  "INSERT INTO users VALUES (1)",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrNotSelect)
			},
		},
		{
			name: "RejectsMultiStatementSQL",
			sql:  "SELECT id FROM input; SELECT 1",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrMultiStatement)
			},
		},
		{
			name: "AcceptsNonRecursiveCTE",
			sql:  "WITH active AS (SELECT id, email FROM input WHERE status = 'active') SELECT email FROM active",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsRecursiveCTE",
			sql:  "WITH RECURSIVE active AS (SELECT id FROM input) SELECT id FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, "recursive CTEs are not allowed")
			},
		},
		{
			name: "RejectsCTEForwardReference",
			sql:  "WITH active AS (SELECT id FROM later), later AS (SELECT id FROM input) SELECT id FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, `references cte "later" before it is defined`)
			},
		},
		{
			name: "RejectsSelfReferencingCTE",
			sql:  "WITH active AS (SELECT id FROM active) SELECT id FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, `cte "active" cannot reference itself`)
			},
		},
		{
			name: "RejectsDuplicateCTEName",
			sql:  "WITH active AS (SELECT id FROM input), active AS (SELECT id FROM input) SELECT id FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, `duplicate cte name "active"`)
			},
		},
		{
			name: "RejectsCTEColumnNameList",
			sql:  "WITH active(email) AS (SELECT email FROM input) SELECT email FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, `column name lists are not supported`)
			},
		},
		{
			name: "RejectsCTESetOperationBody",
			sql:  "WITH active AS (SELECT id FROM input UNION SELECT id FROM input) SELECT id FROM active",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(t, err, "only flat UNION ALL")
			},
		},
		{
			name: "AllowsJoinAcrossNamedInputs",
			sql:  "SELECT a.id FROM input a JOIN other b ON a.id = b.id",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsSubquery",
			sql:  "SELECT (SELECT 1) AS n FROM input",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
			},
		},
		{
			name: "RejectsCountWithDerivedArgument",
			sql:  "SELECT COUNT(CAST(id AS CHAR)) AS total FROM input",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(
					t,
					err,
					"count aggregate supports only column references at runtime",
				)
			},
		},
		{
			name: "AcceptsSumWithNumericCastArgument",
			sql:  "SELECT SUM(CAST(id AS DOUBLE)) AS total FROM input",
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsSumWithNonNumericCastArgument",
			sql:  "SELECT SUM(CAST(id AS CHAR)) AS total FROM input",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrFeatureNotAllowed)
				require.ErrorContains(
					t,
					err,
					"sum aggregate cast argument must target a numeric type",
				)
			},
		},
		{
			name: "RejectsEmptySQL",
			sql:  "   ",
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrRequired)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ValidateTransformSQL(tc.sql)
			tc.assertErr(t, err)
		})
	}
}

func TestDecodeTransformOperationConfig(t *testing.T) {
	t.Parallel()

	t.Run("RejectsNilTransform", func(t *testing.T) {
		t.Parallel()

		decoded, err := decodeTransformOperationConfig(nil)
		require.Nil(t, decoded)
		require.ErrorIs(t, err, xerror.UnexpectedTransformSpecRequired())
	})

	t.Run("RejectsEmptySQL", func(t *testing.T) {
		t.Parallel()

		decoded, err := decodeTransformOperationConfig(&Transform{SQL: "   "})
		require.Nil(t, decoded)
		require.ErrorIs(t, err, ErrRequired)
		require.ErrorContains(t, err, "decode transform operation config")
	})

	t.Run("RejectsUnsupportedOutputKindValue", func(t *testing.T) {
		t.Parallel()

		decoded, err := decodeTransformOperationConfig(&Transform{
			SQL:        "SELECT id FROM input",
			OutputKind: model.TransformOutputKind(99),
		})
		require.Nil(t, decoded)
		require.ErrorContains(t, err, "unsupported output_kind value")
	})

	t.Run("DecodesKnownSQLV1", func(t *testing.T) {
		t.Parallel()

		decoded, err := decodeTransformOperationConfig(&Transform{
			SQL:        "SELECT id FROM input",
			OutputKind: model.TransformOutputKindRows,
			InputContract: &pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "id",
				Type:     model.ContractFieldTypeValueInteger,
				Required: true,
			}}},
		})

		require.NoError(t, err)
		require.NotNil(t, decoded)
		require.Equal(t, transformOperationKindSQLV1, decoded.kind)
		require.NotNil(t, decoded.sqlV1)
		require.Equal(t, "SELECT id FROM input", decoded.sqlV1.SQL)
		require.Equal(t, model.TransformOutputKindRows, decoded.sqlV1.OutputKind)
		require.NotNil(t, decoded.sqlV1.InputSchema)
		require.Len(t, decoded.sqlV1.InputSchema.Fields, 1)
	})
}

func TestValidateTransformSQL_ForbidsWindowFunctions(t *testing.T) {
	t.Parallel()

	_, err := ValidateTransformSQL(
		"SELECT id, RANK() OVER (ORDER BY id) AS rn FROM input",
	)
	require.ErrorIs(t, err, ErrFeatureNotAllowed)
}

func TestValidateOutputContractStructure(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		contract  *pipepayload.Contract
		assertErr func(t *testing.T, err error)
	}{
		{
			name: "AcceptsMatchingAliasedProjection",
			sql:  "SELECT CAST(1 AS SIGNED) AS id, 'alice@example.com' AS email FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsMissingRequiredField",
			sql:  "SELECT CAST(1 AS SIGNED) AS id FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractMissing)
				require.ErrorContains(t, err, "required output contract field \"email\" is missing")
			},
		},
		{
			name: "AcceptsOutputContractDespiteTypeDifference",
			sql:  "SELECT CAST('1' AS CHAR) AS id FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsDerivedExpressionWithoutAlias",
			sql:  "SELECT CAST(1 AS SIGNED) FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractInvalid)
				require.ErrorContains(t, err, "derived select expressions must use an alias")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateOutputContractStructure(tc.sql, tc.contract)
			tc.assertErr(t, err)
		})
	}
}

func TestValidateOutputContractVectors(t *testing.T) {
	t.Parallel()

	t.Run("RequiredIntegerFieldAcceptsIntegerProjection", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"SELECT CAST(1 AS SIGNED) AS id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "id",
				Type:     model.ContractFieldTypeValueInteger,
				Required: true,
			}}},
			model.DataSourceTypeMySQL,
			BuildTransformInputFieldTypes(model.DataSourceTypeMySQL, map[string]string{}),
		)
		require.NoError(t, err)
	})

	t.Run("RequiredIntegerFieldRejectsStringProjection", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"SELECT CAST('1' AS CHAR) AS id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "id",
				Type:     model.ContractFieldTypeValueInteger,
				Required: true,
			}}},
			model.DataSourceTypeMySQL,
			BuildTransformInputFieldTypes(model.DataSourceTypeMySQL, map[string]string{}),
		)
		require.ErrorIs(t, err, ErrContractType)
		require.ErrorContains(t, err, "expects integer")
		require.ErrorContains(t, err, "resolves to string")
	})

	t.Run("MissingRequiredFieldRejected", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractStructure(
			"SELECT CAST(1 AS SIGNED) AS id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "email",
				Type:     model.ContractFieldTypeValueString,
				Required: true,
			}}},
		)
		require.ErrorIs(t, err, ErrContractMissing)
		require.ErrorContains(t, err, "required output contract field \"email\" is missing")
	})

	t.Run("OptionalFieldMayBeMissing", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractStructure(
			"SELECT CAST(1 AS SIGNED) AS id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "score",
				Type:     model.ContractFieldTypeValueNumber,
				Required: false,
			}}},
		)
		require.NoError(t, err)
	})

	t.Run("NestedPathPassesWhenRootSegmentInProjection", func(t *testing.T) {
		t.Parallel()

		// profile.email is required; SQL projects "profile" as the root column.
		// Load-time check should match on the root segment only.
		err := ValidateOutputContractStructure(
			"SELECT profile FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "profile.email",
				Type:     model.ContractFieldTypeValueString,
				Required: true,
			}}},
		)
		require.NoError(t, err)
	})

	t.Run("NestedPathFailsWhenRootSegmentMissingFromProjection", func(t *testing.T) {
		t.Parallel()

		// profile.email is required but SQL does not project "profile".
		err := ValidateOutputContractStructure(
			"SELECT id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "profile.email",
				Type:     model.ContractFieldTypeValueString,
				Required: true,
			}}},
		)
		require.ErrorIs(t, err, ErrContractMissing)
		require.ErrorContains(t, err, "profile.email")
	})

	t.Run("ArrayNestedPathPassesWhenRootSegmentInProjection", func(t *testing.T) {
		t.Parallel()

		// items[].sku is required; SQL projects "items" as root column.
		err := ValidateOutputContractStructure(
			"SELECT items FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name:     "items[].sku",
				Type:     model.ContractFieldTypeValueString,
				Required: true,
			}}},
		)
		require.NoError(t, err)
	})
}

func TestValidateOutputContractTypesWithInputSchema_ExpressionInference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		contract       *pipepayload.Contract
		dataSourceType model.DataSourceType
		assertErr      func(t *testing.T, err error)
	}{
		{
			name: "RejectsUnknownDatasourceType",
			sql:  "SELECT COUNT(*) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			dataSourceType: model.DataSourceTypeUnknown,
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrTypeInput)
			},
		},
		{
			name: "RejectsTypeMismatchForDeterministicAggregate",
			sql:  "SELECT COUNT(*) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			dataSourceType: model.DataSourceTypePostgres,
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, "expects string")
				require.ErrorContains(t, err, "resolves to integer")
			},
		},
		{
			name: "AcceptsIntegerAsNumberCompatibility",
			sql:  "SELECT COUNT(*) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueNumber, Required: true},
			}},
			dataSourceType: model.DataSourceTypeMySQL,
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "RejectsUncertainTypeInference",
			sql:  "SELECT id AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			dataSourceType: model.DataSourceTypePostgres,
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "total" has uncertain projection type`)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateOutputContractTypesWithInputSchema(tc.sql, tc.contract, tc.dataSourceType, nil)
			tc.assertErr(t, err)
		})
	}
}

func TestValidateOutputContractTypesWithResolvedRowContract(t *testing.T) {
	t.Parallel()

	rowTypes := map[string]model.ContractFieldType{
		"account_id":  model.ContractFieldTypeValueString,
		"email":       model.ContractFieldTypeValueString,
		"given_name":  model.ContractFieldTypeValueString,
		"family_name": model.ContractFieldTypeValueString,
		"active":      model.ContractFieldTypeValueBoolean,
	}
	contactContract := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "firstname", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "lastname", Type: model.ContractFieldTypeValueString, Required: true},
	}}

	testCases := []struct {
		name           string
		dataSourceType model.DataSourceType
		sql            string
		contract       *pipepayload.Contract
		inputTypes     map[string]model.ContractFieldType
		wantErr        error
		errContains    string
	}{
		{
			name:           "HubSpotCount",
			dataSourceType: model.DataSourceTypeHubSpot,
			sql:            "SELECT COUNT(*) AS record_count FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "record_count", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
		},
		{
			name:           "HubSpotExplicitIntegerCast",
			dataSourceType: model.DataSourceTypeHubSpot,
			sql:            "SELECT CAST(COUNT(*) AS SIGNED) AS record_count FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "record_count", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
		},
		{
			name:           "GitDirectProjectionAliasAndFilter",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT email, given_name AS firstname, family_name AS lastname FROM input WHERE active = true LIMIT 1",
			contract:       contactContract,
			inputTypes:     rowTypes,
		},
		{
			name:           "GitHubDirectProjectionAliasAndFilter",
			dataSourceType: model.DataSourceTypeGithub,
			sql:            "SELECT email, given_name AS firstname, family_name AS lastname FROM input WHERE active = true LIMIT 1",
			contract:       contactContract,
			inputTypes:     rowTypes,
		},
		{
			name:           "GitLabSupportedCast",
			dataSourceType: model.DataSourceTypeGitlab,
			sql:            "SELECT CAST(account_id AS SIGNED) AS account_id FROM input WHERE active = true",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "account_id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			inputTypes: rowTypes,
		},
		{
			name:           "GitHubUnknownFieldFailsClosed",
			dataSourceType: model.DataSourceTypeGithub,
			sql:            "SELECT missing AS email FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrTypeInput,
			errContains: `unknown column reference "missing"`,
		},
		{
			name:           "GitCastUnknownFieldFailsClosed",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT CAST(missing AS SIGNED) AS account_id FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "account_id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrTypeInput,
			errContains: `unknown column reference "missing"`,
		},
		{
			name:           "GitCountUnknownFieldFailsClosed",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT COUNT(missing) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrTypeInput,
			errContains: `unknown column reference "missing"`,
		},
		{
			name:           "GitSumUnknownFieldFailsClosed",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT SUM(missing) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueNumber, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrTypeInput,
			errContains: `unknown column reference "missing"`,
		},
		{
			name:           "GitAvgUnknownFieldFailsClosed",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT AVG(missing) AS total FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "total", Type: model.ContractFieldTypeValueNumber, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrTypeInput,
			errContains: `unknown column reference "missing"`,
		},
		{
			name:           "GitLabContractMismatchFailsClosed",
			dataSourceType: model.DataSourceTypeGitlab,
			sql:            "SELECT email AS email FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrContractType,
			errContains: "expects integer but projection resolves to string",
		},
		{
			name:           "GitUncertainExpressionFailsClosed",
			dataSourceType: model.DataSourceTypeGit,
			sql:            "SELECT NULL AS email FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			inputTypes:  rowTypes,
			wantErr:     ErrContractType,
			errContains: `output contract field "email" has uncertain projection type`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateOutputContractTypesWithInputSchema(
				tc.sql,
				tc.contract,
				tc.dataSourceType,
				tc.inputTypes,
			)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

func TestInferTransformRowFieldType_AggregatesFailClosed(t *testing.T) {
	t.Parallel()

	inputTypes := transformInputFieldTypes{
		"amount": model.ContractFieldTypeValueNumber,
		"email":  model.ContractFieldTypeValueString,
	}
	testCases := []struct {
		name       string
		expression string
		want       model.ContractFieldType
	}{
		{name: "CountStar", expression: "COUNT(*)", want: model.ContractFieldTypeValueInteger},
		{name: "CountKnownString", expression: "COUNT(email)", want: model.ContractFieldTypeValueInteger},
		{name: "CountUnknown", expression: "COUNT(missing)", want: model.ContractFieldTypeUnknown},
		{name: "SumUnknown", expression: "SUM(missing)", want: model.ContractFieldTypeUnknown},
		{name: "AverageUnknown", expression: "AVG(missing)", want: model.ContractFieldTypeUnknown},
		{name: "SumString", expression: "SUM(email)", want: model.ContractFieldTypeUnknown},
		{name: "AverageString", expression: "AVG(email)", want: model.ContractFieldTypeUnknown},
		{name: "MaximumString", expression: "MAX(email)", want: model.ContractFieldTypeUnknown},
		{name: "MinimumString", expression: "MIN(email)", want: model.ContractFieldTypeUnknown},
		{name: "MaximumNumber", expression: "MAX(amount)", want: model.ContractFieldTypeValueNumber},
		{name: "MinimumNumber", expression: "MIN(amount)", want: model.ContractFieldTypeValueNumber},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			selectStmt, _, err := parseTransformSelect("SELECT " + tc.expression + " AS value FROM input")
			require.NoError(t, err)
			require.Len(t, selectStmt.Fields.Fields, 1)

			require.Equal(
				t,
				tc.want,
				inferTransformRowFieldType(selectStmt.Fields.Fields[0].Expr, inputTypes, nil),
			)
		})
	}
}

func TestValidateOutputContractTypesWithInputSchema_JSONConstructionHelpers(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name           string
		dataSourceType model.DataSourceType
		nativeTypes    map[string]string
		sql            string
		contract       *pipepayload.Contract
		assertErr      func(t *testing.T, err error)
	}

	validNestedContract := &pipepayload.Contract{Fields: []pipepayload.ContractField{
		{Name: "profile", Type: model.ContractFieldTypeValueObject, Required: true},
		{Name: "profile.contact.email", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "items", Type: model.ContractFieldTypeValueArray, Required: true},
		{Name: "items[].sku", Type: model.ContractFieldTypeValueString, Required: true},
		{Name: "items[].quantity", Type: model.ContractFieldTypeValueInteger, Required: true},
	}}
	sqlTextDefault := "SELECT JSON_OBJECT('contact', JSON_OBJECT('email', email), 'id', id) AS profile, JSON_ARRAY(JSON_OBJECT('sku', email, 'quantity', id)) AS items FROM input"

	testCases := []testCase{
		{
			name:           "PostgresAcceptsNestedPaths",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			contract:       validNestedContract,
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:           "HTTPAcceptsNestedPaths",
			dataSourceType: model.DataSourceTypeHTTP,
			nativeTypes:    map[string]string{"id": "integer", "email": "string"},
			contract:       validNestedContract,
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:           "MongoDBAcceptsNestedPaths",
			dataSourceType: model.DataSourceTypeMongoDB,
			nativeTypes:    map[string]string{"id": "int", "email": "string"},
			contract:       validNestedContract,
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:           "RejectsNestedObjectTypeMismatch",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "profile.contact.email", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "profile.contact.email" expects integer but projection resolves to string`)
			},
		},
		{
			name:           "RejectsNestedArrayElementTypeMismatch",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "items[].quantity", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "items[].quantity" expects string but projection resolves to integer`)
			},
		},
		{
			name:           "AcceptsNestedArrayFieldsSharedByEveryElement",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			sql:            "SELECT JSON_ARRAY(JSON_OBJECT('sku', email, 'quantity', id), JSON_OBJECT('sku', email, 'quantity', id)) AS items FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "items[].sku", Type: model.ContractFieldTypeValueString, Required: true},
				{Name: "items[].quantity", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:           "RejectsNestedArrayFieldWhenArrayHasNoElements",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			sql:            "SELECT JSON_ARRAY() AS items FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "items", Type: model.ContractFieldTypeValueArray, Required: true},
				{Name: "items[].sku", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "items[].sku" has uncertain projection type`)
			},
		},
		{
			name:           "RejectsNestedArrayFieldsSplitAcrossDifferentElements",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			sql:            "SELECT JSON_ARRAY(JSON_OBJECT('sku', email), JSON_OBJECT('quantity', id)) AS items FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "items[].sku", Type: model.ContractFieldTypeValueString, Required: true},
				{Name: "items[].quantity", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "items[].sku" has uncertain projection type`)
			},
		},
		{
			name:           "RejectsNestedArrayFieldTypeConflictAcrossElements",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			sql:            "SELECT JSON_ARRAY(JSON_OBJECT('sku', email), JSON_OBJECT('sku', id)) AS items FROM input",
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "items[].sku", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "items[].sku" has uncertain projection type`)
			},
		},
		{
			name:           "RejectsUnprovableNestedPath",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "profile.contact.phone", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			assertErr: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrContractType)
				require.ErrorContains(t, err, `output contract field "profile.contact.phone" has uncertain projection type`)
			},
		},
		{
			name:           "AllowsOptionalUnprovableNestedPath",
			dataSourceType: model.DataSourceTypePostgres,
			nativeTypes:    map[string]string{"id": "integer", "email": "varchar"},
			contract: &pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "profile.contact.phone", Type: model.ContractFieldTypeValueString, Required: false},
			}},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sqlText := tc.sql
			if sqlText == "" {
				sqlText = sqlTextDefault
			}

			err := ValidateOutputContractTypesWithInputSchemaForOutputKind(
				sqlText,
				model.TransformOutputKindJSON,
				tc.contract,
				tc.dataSourceType,
				BuildTransformInputFieldTypes(tc.dataSourceType, tc.nativeTypes),
			)
			tc.assertErr(t, err)
		})
	}
}

func TestValidateOutputContractTypesWithInputSchemaForOutputKind_JSONConstructionRowsStringifiesHelpers(t *testing.T) {
	t.Parallel()

	inputTypes := BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
		"id": "integer",
	})

	err := ValidateOutputContractTypesWithInputSchemaForOutputKind(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindRows,
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchemaForOutputKind(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindRows,
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload",
			Type:     model.ContractFieldTypeValueObject,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.ErrorIs(t, err, ErrContractType)
	require.ErrorContains(t, err, "expects object")
	require.ErrorContains(t, err, "resolves to string")

	err = ValidateOutputContractTypesWithInputSchemaForOutputKind(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindJSON,
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload",
			Type:     model.ContractFieldTypeValueObject,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchemaForOutputKind(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindUnknown,
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload",
			Type:     model.ContractFieldTypeValueString,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchemaForOutputKind(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindUnknown,
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload",
			Type:     model.ContractFieldTypeValueObject,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.ErrorIs(t, err, ErrContractType)
	require.ErrorContains(t, err, "expects object")
	require.ErrorContains(t, err, "resolves to string")

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload.id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: true,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.ErrorIs(t, err, ErrContractType)
	require.ErrorContains(t, err, "constructed JSON projection")
	require.ErrorContains(t, err, "row-oriented output")

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{{
			Name:     "payload.id",
			Type:     model.ContractFieldTypeValueInteger,
			Required: false,
		}}},
		model.DataSourceTypePostgres,
		inputTypes,
	)
	require.NoError(t, err)
}

func TestInferOutputContractWithInputSchema_SerializedAndNullableFields(t *testing.T) {
	t.Parallel()

	rowsContract, err := InferOutputContractWithInputSchema(
		"SELECT email AS email FROM input",
		model.TransformOutputKindRows,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{"email": model.ContractFieldTypeValueString},
	)
	require.NoError(t, err)
	require.Len(t, rowsContract.Fields, 1)
	require.Equal(t, model.ContractFieldTypeValueString, rowsContract.Fields[0].Type)
	require.True(t, rowsContract.Fields[0].Nullable)

	jsonHelperRowsContract, err := InferOutputContractWithInputSchema(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindRows,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{"id": model.ContractFieldTypeValueInteger},
	)
	require.NoError(t, err)
	require.Len(t, jsonHelperRowsContract.Fields, 1)
	require.Equal(t, model.ContractFieldTypeValueString, jsonHelperRowsContract.Fields[0].Type)

	jsonHelperUnknownContract, err := InferOutputContractWithInputSchema(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindUnknown,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{"id": model.ContractFieldTypeValueInteger},
	)
	require.NoError(t, err)
	require.Len(t, jsonHelperUnknownContract.Fields, 1)
	require.Equal(t, model.ContractFieldTypeValueString, jsonHelperUnknownContract.Fields[0].Type)

	jsonHelperDocumentContract, err := InferOutputContractWithInputSchema(
		"SELECT JSON_OBJECT('id', id) AS payload FROM input",
		model.TransformOutputKindJSON,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{"id": model.ContractFieldTypeValueInteger},
	)
	require.NoError(t, err)
	require.Len(t, jsonHelperDocumentContract.Fields, 1)
	require.Equal(t, model.ContractFieldTypeValueObject, jsonHelperDocumentContract.Fields[0].Type)

	csvContract, err := InferOutputContractWithInputSchema(
		"SELECT id AS id FROM input",
		model.TransformOutputKindCSV,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{"id": model.ContractFieldTypeValueInteger},
	)
	require.NoError(t, err)
	require.Equal(t, model.ContractRepresentationCSV, csvContract.Representation)
	require.Len(t, csvContract.Fields, 1)
	require.Equal(t, model.ContractFieldTypeValueString, csvContract.Fields[0].Type)
	require.True(t, csvContract.Fields[0].Nullable)

	cteContract, err := InferOutputContractWithInputSchema(
		"WITH active AS (SELECT id, email FROM input WHERE status = 'active') SELECT email AS active_email FROM active",
		model.TransformOutputKindRows,
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"input.id":     model.ContractFieldTypeValueInteger,
			"input.email":  model.ContractFieldTypeValueString,
			"input.status": model.ContractFieldTypeValueString,
		},
	)
	require.NoError(t, err)
	require.Len(t, cteContract.Fields, 1)
	require.Equal(t, "active_email", cteContract.Fields[0].Name)
	require.Equal(t, model.ContractFieldTypeValueString, cteContract.Fields[0].Type)
	require.True(t, cteContract.Fields[0].Nullable)
}

func TestValidateOutputContractTypesWithInputSchema_JSONLikeAndBSONLiteralInference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		dataSourceType model.DataSourceType
	}{
		{name: "HTTP", dataSourceType: model.DataSourceTypeHTTP},
		{name: "FTP", dataSourceType: model.DataSourceTypeFTP},
		{name: "HubSpot", dataSourceType: model.DataSourceTypeHubSpot},
		{name: "MongoDB", dataSourceType: model.DataSourceTypeMongoDB},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateOutputContractTypesWithInputSchema(
				"SELECT 'active' AS status FROM input",
				&pipepayload.Contract{Fields: []pipepayload.ContractField{{
					Name:     "status",
					Type:     model.ContractFieldTypeValueString,
					Required: true,
				}}},
				tc.dataSourceType,
				BuildTransformInputFieldTypes(tc.dataSourceType, map[string]string{
					"id": "string",
				}),
			)
			require.NoError(t, err)
		})
	}
}

func TestValidateOutputContractTypesWithInputSchema_AliasedInputInference(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT o.id AS order_id, c.email AS customer_email FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "order_id", Type: model.ContractFieldTypeValueInteger, Required: true},
			{Name: "customer_email", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"orders.id":          model.ContractFieldTypeValueInteger,
			"orders.customer_id": model.ContractFieldTypeValueInteger,
			"customers.id":       model.ContractFieldTypeValueInteger,
			"customers.email":    model.ContractFieldTypeValueString,
		},
	)
	require.NoError(t, err)
}

func TestValidateOutputContractTypesWithInputSchema_CTEInference(t *testing.T) {
	t.Parallel()

	inputTypes := transformInputFieldTypes{
		"input.id":     model.ContractFieldTypeValueInteger,
		"input.email":  model.ContractFieldTypeValueString,
		"input.status": model.ContractFieldTypeValueString,
	}

	t.Run("FinalSelectReferencesCTEFields", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"WITH active AS (SELECT id, email FROM input WHERE status = 'active') SELECT email AS email FROM active",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			model.DataSourceTypePostgres,
			inputTypes,
		)
		require.NoError(t, err)
	})

	t.Run("DownstreamCTEReferencesPriorCTE", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"WITH active AS (SELECT id, email FROM input WHERE status = 'active'), picked AS (SELECT email FROM active WHERE id > 1) SELECT email AS email FROM picked",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			model.DataSourceTypePostgres,
			inputTypes,
		)
		require.NoError(t, err)
	})

	t.Run("TypeMismatchFromCTEFieldFailsClosed", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"WITH active AS (SELECT id, email FROM input WHERE status = 'active') SELECT email AS email FROM active",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "email", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			model.DataSourceTypePostgres,
			inputTypes,
		)
		require.ErrorIs(t, err, ErrContractType)
		require.ErrorContains(t, err, "expects integer")
		require.ErrorContains(t, err, "resolves to string")
	})

	t.Run("CTENameShadowingDeclaredInputFailsClosed", func(t *testing.T) {
		t.Parallel()

		err := ValidateOutputContractTypesWithInputSchema(
			"WITH input AS (SELECT id FROM source) SELECT id AS id FROM input",
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			}},
			model.DataSourceTypePostgres,
			transformInputFieldTypes{
				"input.id":  model.ContractFieldTypeValueInteger,
				"source.id": model.ContractFieldTypeValueInteger,
			},
		)
		require.ErrorIs(t, err, ErrTypeInput)
		require.ErrorContains(t, err, `cte name "input" shadows transform input relation`)
	})
}

func TestValidateOutputContractTypesWithInputSchema_AliasInferenceRejectsUnknownRelations(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT invoices.id AS id FROM invoices",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
		}},
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"id":       model.ContractFieldTypeValueInteger,
			"input.id": model.ContractFieldTypeValueInteger,
		},
	)
	require.ErrorIs(t, err, ErrTypeInput)
	require.ErrorContains(t, err, `unknown transform input "invoices"`)
}

func TestValidateOutputContractTypesWithInputSchema_AliasedInputInferenceDeduplicatesUnqualifiedColumns(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT email AS customer_email FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "customer_email", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"orders.id":          model.ContractFieldTypeValueInteger,
			"orders.customer_id": model.ContractFieldTypeValueInteger,
			"customers.id":       model.ContractFieldTypeValueInteger,
			"customers.email":    model.ContractFieldTypeValueString,
		},
	)
	require.NoError(t, err)
}

func TestValidateOutputContractTypesWithInputSchema_UnqualifiedLegacyTypeResolvesWithMultipleRelations(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT email AS email FROM orders CROSS JOIN customers",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"email": model.ContractFieldTypeValueString,
		},
	)
	require.NoError(t, err)
}

func TestValidateOutputContractTypesWithInputSchema_UnqualifiedMissingColumnWithMultipleRelationsFailsUnknown(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT missing AS missing FROM orders CROSS JOIN customers",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "missing", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		transformInputFieldTypes{
			"orders.id":       model.ContractFieldTypeValueInteger,
			"customers.email": model.ContractFieldTypeValueString,
		},
	)
	require.ErrorIs(t, err, ErrTypeInput)
	require.ErrorContains(t, err, `unknown column reference "missing"`)
	require.NotContains(t, err.Error(), "ambiguous")
}

func TestValidateOutputContractTypesWithInputSchema(t *testing.T) {
	t.Parallel()

	err := ValidateOutputContractTypesWithInputSchema(
		"SELECT id AS id, name AS name FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			{Name: "name", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
			"id":   "integer",
			"name": "varchar",
		}),
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT amount + 1 AS amount_inc FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "amount_inc", Type: model.ContractFieldTypeValueNumber, Required: true},
		}},
		model.DataSourceTypePostgres,
		BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
			"amount": "integer",
		}),
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT 'acme' AS org_slug FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "org_slug", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypePostgres,
		BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
			"id": "integer",
		}),
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT SUM(CAST(amount AS DOUBLE)) AS total_amount FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{
				Name:     "total_amount",
				Type:     model.ContractFieldTypeValueNumber,
				Required: true,
			},
		}},
		model.DataSourceTypePostgres,
		BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
			"amount": "integer",
		}),
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT id AS id, email AS email FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true},
			{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypeFTP,
		BuildTransformInputFieldTypes(model.DataSourceTypeFTP, map[string]string{
			"id":    "integer",
			"email": "string",
		}),
	)
	require.NoError(t, err)

	err = ValidateOutputContractTypesWithInputSchema(
		"SELECT id AS id FROM input",
		&pipepayload.Contract{Fields: []pipepayload.ContractField{
			{Name: "id", Type: model.ContractFieldTypeValueString, Required: true},
		}},
		model.DataSourceTypeFTP,
		BuildTransformInputFieldTypes(model.DataSourceTypeFTP, map[string]string{
			"id": "integer",
		}),
	)
	require.ErrorIs(t, err, ErrContractType)
	require.ErrorContains(t, err, "expects string")
	require.ErrorContains(t, err, "resolves to integer")
}

func TestContractFieldTypeFromNative(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		model.ContractFieldTypeValueInteger,
		ContractFieldTypeFromNative(model.DataSourceTypePostgres, "INTEGER"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueString,
		ContractFieldTypeFromNative(model.DataSourceTypeMySQL, "varchar(255)"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueInteger,
		ContractFieldTypeFromNative(model.DataSourceTypeBigQuery, "INT64"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueString,
		ContractFieldTypeFromNative(model.DataSourceTypeBigQuery, "NUMERIC"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueBoolean,
		ContractFieldTypeFromNative(model.DataSourceTypeMongoDB, "boolean"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueObject,
		ContractFieldTypeFromNative(model.DataSourceTypeHTTP, "object"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueString,
		ContractFieldTypeFromNative(model.DataSourceTypeHubSpot, "enumeration"),
	)
	require.Equal(
		t,
		model.ContractFieldTypeValueArray,
		ContractFieldTypeFromNative(model.DataSourceTypeFTP, "array"),
	)
	for _, dataSourceType := range []model.DataSourceType{
		model.DataSourceTypeGit,
		model.DataSourceTypeGithub,
		model.DataSourceTypeGitlab,
	} {
		require.Equal(
			t,
			model.ContractFieldTypeValueBoolean,
			ContractFieldTypeFromNative(dataSourceType, "boolean"),
		)
	}
}

func TestTransformSQL_NewFeatureValidationRuntimeParity(t *testing.T) {
	t.Parallel()

	rowsPayload := func(columnNames []string, rows []map[string]any) *pipepayload.Payload {
		return runtimeRowsPayload(
			columnNames,
			len(rows),
			rows,
		)
	}

	t.Run("GroupByAggregateIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT category AS category, COUNT(*) AS total FROM input GROUP BY category"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"category", "amount"}, []map[string]any{
			{"category": "hardware", "amount": 10},
			{"category": "hardware", "amount": 20},
			{"category": "software", "amount": 5},
		})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, 2, transformOutput.RowCount)
	})

	t.Run("NumericCastAggregateArgumentIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT SUM(CAST(amount AS DOUBLE)) AS total_amount FROM input"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		upstream := rowsPayload(
			[]string{"amount"},
			[]map[string]any{{"amount": 10}, {"amount": 20}, {"amount": 5}},
		)

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"total_amount": float64(35)}}, transformOutput.Rows)
	})

	t.Run("AggregateResultCastIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT CAST(COUNT(*) AS SIGNED) AS total FROM input"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		upstream := rowsPayload(
			[]string{"id"},
			[]map[string]any{{"id": 1}, {"id": 2}},
		)

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"total": int64(2)}}, transformOutput.Rows)
	})

	for _, tc := range []struct {
		name    string
		castSQL string
	}{
		{name: "Character", castSQL: "CAST(SUM(amount) AS CHAR)"},
		{name: "Signed", castSQL: "CAST(SUM(amount) AS SIGNED)"},
	} {
		t.Run("AllNullGroupedSumCast"+tc.name, func(t *testing.T) {
			t.Parallel()

			sqlText := "SELECT category, " + tc.castSQL + " AS total FROM input GROUP BY category"
			_, err := ValidateTransformSQL(sqlText)
			require.NoError(t, err)

			upstream := rowsPayload(
				[]string{"category", "amount"},
				[]map[string]any{
					{"category": "empty", "amount": nil},
					{"category": "empty", "amount": nil},
				},
			)

			transformOutput, err := executeTransformStepFromPayload(
				&Transform{SQL: sqlText},
				upstream,
			)
			require.NoError(t, err)
			require.Equal(
				t,
				[]map[string]any{{"category": "empty", "total": nil}},
				transformOutput.Rows,
			)
		})
	}

	t.Run("ArithmeticProjectionIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT amount + 1 AS amount_inc FROM input"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		err = ValidateOutputContractTypesWithInputSchema(
			sqlText,
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{
					Name:     "amount_inc",
					Type:     model.ContractFieldTypeValueNumber,
					Required: true,
				},
			}},
			model.DataSourceTypePostgres,
			BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
				"amount": "integer",
			}),
		)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"amount"}, []map[string]any{{"amount": 1}, {"amount": 2}})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(
			t,
			[]map[string]any{{"amount_inc": int64(2)}, {"amount_inc": int64(3)}},
			transformOutput.Rows,
		)
	})

	t.Run("JSONConstructionProjectionEmitsNativeJSONDocumentValues", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT JSON_OBJECT('contact', JSON_OBJECT('email', email), 'id', id) AS profile, JSON_ARRAY(JSON_OBJECT('sku', email, 'quantity', id)) AS items FROM input"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"id", "email"}, []map[string]any{
			{"id": 1, "email": "alice@example.com"},
		})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText, OutputKind: model.TransformOutputKindJSON},
			upstream,
		)
		require.NoError(t, err)

		require.JSONEq(
			t,
			`[{"profile":{"contact":{"email":"alice@example.com"},"id":1},"items":[{"sku":"alice@example.com","quantity":1}]}]`,
			string(transformOutput.Document),
		)
	})

	t.Run("WhereProjectionIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id AS id FROM input WHERE id > 1"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		err = ValidateOutputContractTypesWithInputSchema(
			sqlText,
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true,
			}}},
			model.DataSourceTypePostgres,
			BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
				"id": "integer",
			}),
		)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"id"}, []map[string]any{{"id": 1}, {"id": 2}, {"id": 3}})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"id": 2}, {"id": 3}}, transformOutput.Rows)
	})

	t.Run("WhereBooleanLiteralComparisonIsRunnable", func(t *testing.T) {
		t.Parallel()

		upstream := rowsPayload(
			[]string{"email", "active"},
			[]map[string]any{
				{"email": "active@example.com", "active": true},
				{"email": "inactive@example.com", "active": false},
			},
		)

		for _, testCase := range []struct {
			name      string
			sql       string
			wantEmail string
		}{
			{name: "true", sql: "SELECT email FROM input WHERE active = true", wantEmail: "active@example.com"},
			{name: "false", sql: "SELECT email FROM input WHERE active = false", wantEmail: "inactive@example.com"},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				transformOutput, err := executeTransformStepFromPayload(
					&Transform{SQL: testCase.sql},
					upstream,
				)
				require.NoError(t, err)
				require.Equal(t, []map[string]any{{"email": testCase.wantEmail}}, transformOutput.Rows)
			})
		}
	})

	t.Run("CTEProjectionIsValidatedTypedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "WITH active AS (SELECT id, email FROM input WHERE status = 'active'), picked AS (SELECT email FROM active WHERE id > 1) SELECT email AS email FROM picked ORDER BY email"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		err = ValidateOutputContractTypesWithInputSchema(
			sqlText,
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name: "email", Type: model.ContractFieldTypeValueString, Required: true,
			}}},
			model.DataSourceTypePostgres,
			transformInputFieldTypes{
				"input.id":     model.ContractFieldTypeValueInteger,
				"input.email":  model.ContractFieldTypeValueString,
				"input.status": model.ContractFieldTypeValueString,
			},
		)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"id", "email", "status"}, []map[string]any{
			{"id": 1, "email": "alice@example.com", "status": "active"},
			{"id": 2, "email": "bob@example.com", "status": "active"},
			{"id": 3, "email": "carol@example.com", "status": "inactive"},
		})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"email": "bob@example.com"}}, transformOutput.Rows)
	})

	t.Run("MultiInputJoinCTEIsRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "WITH paid AS (SELECT orders.id AS order_id, customers.email AS email FROM orders JOIN customers ON orders.customer_id = customers.id WHERE orders.status = 'paid') SELECT order_id AS order_id, email AS email FROM paid"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		err = ValidateOutputContractTypesWithInputSchema(
			sqlText,
			&pipepayload.Contract{Fields: []pipepayload.ContractField{
				{Name: "order_id", Type: model.ContractFieldTypeValueInteger, Required: true},
				{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
			}},
			model.DataSourceTypePostgres,
			transformInputFieldTypes{
				"orders.id":          model.ContractFieldTypeValueInteger,
				"orders.customer_id": model.ContractFieldTypeValueInteger,
				"orders.status":      model.ContractFieldTypeValueString,
				"customers.id":       model.ContractFieldTypeValueInteger,
				"customers.email":    model.ContractFieldTypeValueString,
			},
		)
		require.NoError(t, err)

		transformOutput, err := executeTransformStepFromPayloads(
			&Transform{SQL: sqlText},
			map[string]*pipepayload.Payload{
				"orders": rowsPayload(
					[]string{"id", "customer_id", "status"},
					[]map[string]any{
						{"id": 10, "customer_id": 1, "status": "paid"},
						{"id": 11, "customer_id": 2, "status": "pending"},
					},
				),
				"customers": rowsPayload(
					[]string{"id", "email"},
					[]map[string]any{
						{"id": 1, "email": "ada@example.com"},
						{"id": 2, "email": "grace@example.com"},
					},
				),
			},
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"order_id": 10, "email": "ada@example.com"}}, transformOutput.Rows)
	})

	t.Run("CTENameShadowingInputFailsClosedAtRuntime", func(t *testing.T) {
		t.Parallel()

		sqlText := "WITH input AS (SELECT id FROM other) SELECT id AS id FROM input"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		_, err = executeTransformStepFromPayloads(
			&Transform{SQL: sqlText},
			map[string]*pipepayload.Payload{
				"input": rowsPayload([]string{"id"}, []map[string]any{{"id": 1}}),
				"other": rowsPayload([]string{"id"}, []map[string]any{{"id": 2}}),
			},
		)
		require.ErrorIs(t, err, ErrFeatureNotAllowed)
		require.ErrorContains(t, err, `cte name "input" shadows an existing transform relation`)
	})

	t.Run("QualifiedDuplicateJoinColumnsAreRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT users.id AS user_id, accounts.id AS account_id FROM users JOIN accounts ON accounts.user_id = users.id"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		transformOutput, err := executeTransformStepFromPayloads(
			&Transform{SQL: sqlText},
			map[string]*pipepayload.Payload{
				"users": rowsPayload(
					[]string{"id", "email"},
					[]map[string]any{{"id": 1, "email": "ada@example.com"}},
				),
				"accounts": rowsPayload(
					[]string{"id", "user_id", "name"},
					[]map[string]any{{"id": 10, "user_id": 1, "name": "Acme"}},
				),
			},
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"user_id": 1, "account_id": 10}}, transformOutput.Rows)
	})

	t.Run("UnqualifiedDuplicateJoinColumnFailsAmbiguous", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id FROM users JOIN accounts ON accounts.user_id = users.id"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		_, err = executeTransformStepFromPayloads(
			&Transform{SQL: sqlText},
			map[string]*pipepayload.Payload{
				"users": rowsPayload(
					[]string{"id", "email"},
					[]map[string]any{{"id": 1, "email": "ada@example.com"}},
				),
				"accounts": rowsPayload(
					[]string{"id", "user_id", "name"},
					[]map[string]any{{"id": 10, "user_id": 1, "name": "Acme"}},
				),
			},
		)
		require.Error(t, err)
		require.ErrorContains(t, err, `field "id" is ambiguous`)
	})

	t.Run("OrderByAndLimitProjectionIsValidatedAndRunnable", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id AS id FROM input ORDER BY id DESC LIMIT 2"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		err = ValidateOutputContractTypesWithInputSchema(
			sqlText,
			&pipepayload.Contract{Fields: []pipepayload.ContractField{{
				Name: "id", Type: model.ContractFieldTypeValueInteger, Required: true,
			}}},
			model.DataSourceTypePostgres,
			BuildTransformInputFieldTypes(model.DataSourceTypePostgres, map[string]string{
				"id": "integer",
			}),
		)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"id"}, []map[string]any{{"id": 1}, {"id": 3}, {"id": 2}})

		transformOutput, err := executeTransformStepFromPayload(
			&Transform{SQL: sqlText},
			upstream,
		)
		require.NoError(t, err)

		require.Equal(t, []map[string]any{{"id": 3}, {"id": 2}}, transformOutput.Rows)
	})

	t.Run("OrderByUnsupportedExpressionRejectsBeforeRuntime", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id AS id FROM input ORDER BY ABS(id) DESC"
		_, err := ValidateTransformSQL(sqlText)
		require.ErrorIs(t, err, ErrFeatureNotAllowed)

		upstream := rowsPayload([]string{"id"}, []map[string]any{{"id": 1}, {"id": 3}, {"id": 2}})

		_, err = executeTransformStepFromPayload(&Transform{SQL: sqlText}, upstream)
		require.ErrorIs(t, err, ErrFeatureNotAllowed)
	})

	t.Run("LimitInvalidLiteralIsRejectedByValidation", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id AS id FROM input LIMIT 'x'"
		_, err := ValidateTransformSQL(sqlText)
		require.Error(t, err)
		require.ErrorContains(t, err, "parse transform sql")
	})

	t.Run("OrderByIncomparableValuesFailsClosedAtRuntime", func(t *testing.T) {
		t.Parallel()

		sqlText := "SELECT id AS id FROM input ORDER BY id DESC"
		_, err := ValidateTransformSQL(sqlText)
		require.NoError(t, err)

		upstream := rowsPayload([]string{"id"}, []map[string]any{
			{"id": map[string]any{"v": 1}},
			{"id": map[string]any{"v": 2}},
		})

		_, err = executeTransformStepFromPayload(&Transform{SQL: sqlText}, upstream)
		require.Error(t, err)
		require.ErrorContains(t, err, "ordered WHERE comparison is unsupported")
	})
}

func TestExecuteTransformStepFromPayload_UpstreamPayloadKinds(t *testing.T) {
	t.Parallel()

	rowsPayload := func(columnNames []string, rows []map[string]any) *pipepayload.Payload {
		return runtimeRowsPayload(
			columnNames,
			len(rows),
			rows,
		)
	}

	jsonPayload := func(body string) *pipepayload.Payload {
		return &pipepayload.Payload{
			Representation: model.ContractRepresentationJSON,
			RootShape:      model.ContractRootShapeUnknown,
			Document:       []byte(body),
		}
	}

	t.Run("AcceptsRowsPayloadAsTransformInput", func(t *testing.T) {
		t.Parallel()

		upstream := rowsPayload([]string{"id", "email"}, []map[string]any{
			{"id": "1", "email": "alice@example.com"},
			{"id": "2", "email": "bob@example.com"},
		})

		transformOutput, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT email AS email FROM input",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.NoError(t, err)
		require.Equal(t, 2, transformOutput.RowCount)
		require.Equal(t, []string{"email"}, transformOutput.ColumnNames)
		require.Equal(t, []map[string]any{
			{"email": "alice@example.com"},
			{"email": "bob@example.com"},
		}, transformOutput.Rows)
	})

	t.Run("RejectsNonTabularJSONPayloads", func(t *testing.T) {
		t.Parallel()

		upstream := jsonPayload(`"ok"`)

		_, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT id FROM input",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.Error(t, err)
		require.ErrorContains(t, err, "HTTP response format unsupported")
		require.ErrorContains(t, err, "expected JSON array or object")
	})

	t.Run("AcceptsJSONArrayPayloadAsTransformInput", func(t *testing.T) {
		t.Parallel()

		upstream := jsonPayload(
			`[{"id":1,"email":"alice@example.com"},{"id":2,"email":"bob@example.com"}]`,
		)

		transformOutput, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT email AS email FROM input ORDER BY email",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.NoError(t, err)
		require.Equal(t, 2, transformOutput.RowCount)
		require.Equal(t, []string{"email"}, transformOutput.ColumnNames)
		require.Equal(
			t,
			[]map[string]any{{"email": "alice@example.com"}, {"email": "bob@example.com"}},
			transformOutput.Rows,
		)
	})

	t.Run("AcceptsJSONArrayDocumentPayloadAsTransformInput", func(t *testing.T) {
		t.Parallel()

		upstream := jsonPayload(
			`[{"order_id":"A-1","status":"pending"},{"order_id":"A-2","status":"pending"}]`,
		)

		transformOutput, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT order_id AS order_id FROM input ORDER BY order_id",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.NoError(t, err)
		require.Equal(t, 2, transformOutput.RowCount)
		require.Equal(t, []string{"order_id"}, transformOutput.ColumnNames)
		require.Equal(
			t,
			[]map[string]any{{"order_id": "A-1"}, {"order_id": "A-2"}},
			transformOutput.Rows,
		)
	})

	t.Run("AcceptsJSONExtractIndexPathForHeadlessCSVRowsPayload", func(t *testing.T) {
		t.Parallel()

		upstream := rowsPayload([]string{"csv_row"}, []map[string]any{
			{"csv_row": []any{"alice", 31}},
			{"csv_row": []any{"bob", 29}},
		})

		transformOutput, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT json_extract(csv_row, '$[0]') AS first_col FROM input",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.NoError(t, err)
		require.Equal(t, []map[string]any{
			{"first_col": "alice"},
			{"first_col": "bob"},
		}, transformOutput.Rows)
	})

	t.Run("RejectsJSONExtractNamedPathForHeadlessCSVRowsPayload", func(t *testing.T) {
		t.Parallel()

		upstream := rowsPayload([]string{"csv_row"}, []map[string]any{
			{"csv_row": []any{"alice", 31}},
		})

		_, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT json_extract(csv_row, '$.name') AS name FROM input",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.Error(t, err)
		require.ErrorContains(t, err, "headless CSV/array value")
		require.ErrorIs(t, err, ErrRuntimeHeadlessCSVNamedAccessUnsupported)
	})

	t.Run("RejectsJSONExtractNamedPathAfterArrayIndex", func(t *testing.T) {
		t.Parallel()

		upstream := rowsPayload([]string{"csv_row"}, []map[string]any{
			{"csv_row": []any{map[string]any{"name": "alice"}}},
		})

		_, err := executeTransformStepFromPayload(&Transform{
			SQL:        "SELECT json_extract(csv_row, '$[0].name') AS name FROM input",
			OutputKind: model.TransformOutputKindRows,
		}, upstream)
		require.ErrorIs(t, err, ErrRuntimeHeadlessCSVNamedAccessUnsupported)
	})
}

// TestPayloadToTransformInput covers payload normalization and input contract validation
// for json/csv runtime payloads.
func TestPayloadToTransformInput(t *testing.T) {
	t.Parallel()

	// contract builds a minimal pipepayload.Contract from a list of (name, required) pairs.
	contract := func(fields ...any) *pipepayload.Contract {
		cf := make([]pipepayload.ContractField, 0, len(fields)/2)
		for i := 0; i+1 < len(fields); i += 2 {
			cf = append(cf, pipepayload.ContractField{
				Name:     fields[i].(string),
				Required: fields[i+1].(bool),
			})
		}
		return &pipepayload.Contract{Fields: cf}
	}

	jsonPayload := func(body string) *pipepayload.Payload {
		return &pipepayload.Payload{
			Representation: model.ContractRepresentationJSON,
			RootShape:      model.ContractRootShapeUnknown,
			Document:       []byte(body),
		}
	}

	csvPayload := func(body string) *pipepayload.Payload {
		return &pipepayload.Payload{
			Representation: model.ContractRepresentationCSV,
			RootShape:      model.ContractRootShapeUnknown,
			CSV:            body,
		}
	}

	rowsPayload := func(columnNames []string, rows []map[string]any) *pipepayload.Payload {
		return runtimeRowsPayload(
			columnNames,
			len(rows),
			rows,
		)
	}

	testCases := []struct {
		name     string
		payload  *pipepayload.Payload
		contract *pipepayload.Contract
		wantRows []map[string]any
		wantCols []string
		wantErr  func(t *testing.T, err error)
	}{
		// --- Rows payloads ---
		{
			name: "Rows_Passthrough",
			payload: rowsPayload(
				[]string{"id", "name"},
				[]map[string]any{{"id": 1, "name": "Alice"}},
			),
			contract: nil,
			wantRows: []map[string]any{{"id": 1, "name": "Alice"}},
			wantCols: []string{"id", "name"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "Rows_RequiredFlatFieldMissing_ReturnsError",
			payload: rowsPayload(
				[]string{"id"},
				[]map[string]any{{"id": 1}},
			),
			contract: contract("id", true, "email", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "email"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},

		// --- JSON payloads ---
		{
			name:     "JSON_ArrayFlatObjects_ToRows",
			payload:  jsonPayload(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`),
			contract: nil,
			wantRows: []map[string]any{
				{"id": json.Number("1"), "name": "Alice"},
				{"id": json.Number("2"), "name": "Bob"},
			},
			wantCols: []string{"id", "name"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_ObjectSingleRow",
			payload:  jsonPayload(`{"id":1,"name":"Alice"}`),
			contract: nil,
			wantRows: []map[string]any{{"id": json.Number("1"), "name": "Alice"}},
			wantCols: []string{"id", "name"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "JSON_ArrayPreservesNestedObjects",
			payload: jsonPayload(
				`[{"id":1,"profile":{"name":"Alice","locale":"en"}}]`,
			),
			wantRows: []map[string]any{
				{"id": json.Number("1"), "profile": map[string]any{"name": "Alice", "locale": "en"}},
			},
			wantCols: []string{"id", "profile"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:    "JSON_ArrayPreservesNestedArrayField",
			payload: jsonPayload(`[{"id":1,"tags":["a","b"]}]`),
			wantRows: []map[string]any{
				{"id": json.Number("1"), "tags": []any{"a", "b"}},
			},
			wantCols: []string{"id", "tags"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},

		// --- JSON: unsupported formats ---

		{
			name:    "JSON_ScalarString_ReturnsUnsupportedError",
			payload: jsonPayload(`"just a string"`),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
				require.ErrorContains(t, err, "expected JSON array or object")
			},
		},
		{
			name:    "JSON_Number_ReturnsUnsupportedError",
			payload: jsonPayload(`42`),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
			},
		},
		{
			name:    "JSON_NullBody_ReturnsUnsupportedError",
			payload: jsonPayload(`null`),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
			},
		},
		{
			name:    "JSON_EmptyDocument_ReturnsUnsupportedError",
			payload: jsonPayload(``),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "json runtime payload is empty")
			},
		},
		{
			name:    "JSON_MalformedDocument_ReturnsUnsupportedError",
			payload: jsonPayload(`{bad json`),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
			},
		},
		{
			name:    "JSON_ArrayWithNonObjectElement_ReturnsUnsupportedError",
			payload: jsonPayload(`[1, 2, 3]`),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
			},
		},

		// --- JSON: input contract validation ---

		{
			name:     "JSON_RequiredFlatFieldPresent_Passes",
			payload:  jsonPayload(`[{"id":1,"email":"alice@example.com"}]`),
			contract: contract("id", true, "email", true),
			wantRows: []map[string]any{{"id": json.Number("1"), "email": "alice@example.com"}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_RequiredFlatFieldMissing_ReturnsError",
			payload:  jsonPayload(`[{"id":1}]`),
			contract: contract("id", true, "email", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "email"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},
		{
			name:     "JSON_OptionalFieldMissing_Passes",
			payload:  jsonPayload(`[{"id":1}]`),
			contract: contract("id", true, "email", false),
			wantRows: []map[string]any{{"id": json.Number("1")}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_RequiredNestedPathPresent_Passes",
			payload:  jsonPayload(`[{"id":1,"profile":{"name":"Alice"}}]`),
			contract: contract("profile.name", true),
			wantRows: []map[string]any{
				{"id": json.Number("1"), "profile": map[string]any{"name": "Alice"}},
			},
			wantErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_RequiredNestedPathMissing_ReturnsError",
			payload:  jsonPayload(`[{"id":1,"profile":{}}]`),
			contract: contract("profile.email", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "profile.email"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},
		{
			name:    "JSON_IntegerValidationPreservesPrecision",
			payload: jsonPayload(`[{"id":9007199254740993}]`),
			contract: &pipepayload.Contract{
				Representation: model.ContractRepresentationJSON,
				Fields: []pipepayload.ContractField{{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
					Validations: &pipepayload.ContractFieldValidations{
						Min: json.Number("9007199254740993"),
					},
				}},
			},
			wantRows: []map[string]any{{"id": json.Number("9007199254740993")}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},

		// --- CSV payloads ---

		{
			name:    "CSV_TextParsedAsRows",
			payload: csvPayload("id,name\n1,Alice\n2,Bob\n"),
			wantRows: []map[string]any{
				{"id": "1", "name": "Alice"},
				{"id": "2", "name": "Bob"},
			},
			wantCols: []string{"id", "name"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "CSV_TextParsedAsRowsWithAlternateHeaders",
			payload:  csvPayload("col1,col2\nA,B\n"),
			wantRows: []map[string]any{{"col1": "A", "col2": "B"}},
			wantCols: []string{"col1", "col2"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "CSV_WithContractValidation_Passes",
			payload:  csvPayload("id,email\n1,alice@example.com\n"),
			contract: contract("id", true, "email", true),
			wantRows: []map[string]any{{"id": "1", "email": "alice@example.com"}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:    "CSV_WithIntegerValidationPreservesOriginalRows",
			payload: csvPayload("id,email\n001,alice@example.com\n"),
			contract: &pipepayload.Contract{
				Representation: model.ContractRepresentationCSV,
				Fields: []pipepayload.ContractField{
					{
						Name:     "id",
						Type:     model.ContractFieldTypeValueInteger,
						Required: true,
						Validations: &pipepayload.ContractFieldValidations{
							Min: int64(1),
						},
					},
					{Name: "email", Type: model.ContractFieldTypeValueString, Required: true},
				},
			},
			wantRows: []map[string]any{{"id": "001", "email": "alice@example.com"}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "CSV_MissingRequiredContractField_ReturnsError",
			payload:  csvPayload("id\n1\n"),
			contract: contract("id", true, "email", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "email"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},
		{
			name:    "CSV_EmptyBody_ReturnsUnsupportedError",
			payload: csvPayload(``),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "HTTP response format unsupported")
			},
		},
		{
			name:    "UnknownRepresentation_ReturnsUnsupportedError",
			payload: &pipepayload.Payload{},
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(
					t,
					err,
					"transform runtime currently supports only SQL/FTP/HTTP/MongoDB tabular inputs",
				)
			},
		},

		// --- JSON array document payloads ---

		{
			name:    "JSON_ArrayDocuments_ToRows",
			payload: jsonPayload(`[{"_id":"abc","name":"Alice"},{"_id":"def","name":"Bob"}]`),
			wantRows: []map[string]any{
				{"_id": "abc", "name": "Alice"},
				{"_id": "def", "name": "Bob"},
			},
			wantCols: []string{"_id", "name"},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:    "JSON_ArrayDocuments_PreservesNestedDocument",
			payload: jsonPayload(`[{"_id":"abc","address":{"city":"NY"}}]`),
			wantRows: []map[string]any{
				{"_id": "abc", "address": map[string]any{"city": "NY"}},
			},
			wantErr: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_ArrayDocuments_RequiredFieldPresent_Passes",
			payload:  jsonPayload(`[{"_id":"abc","status":"pending"}]`),
			contract: contract("status", true),
			wantRows: []map[string]any{{"_id": "abc", "status": "pending"}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:    "JSON_ArrayDocuments_IntegerValidationRejectsStringValue",
			payload: jsonPayload(`[{"id":"2"}]`),
			contract: &pipepayload.Contract{
				Representation: model.ContractRepresentationJSON,
				Fields: []pipepayload.ContractField{{
					Name:     "id",
					Type:     model.ContractFieldTypeValueInteger,
					Required: true,
					Validations: &pipepayload.ContractFieldValidations{
						Min: int64(1),
					},
				}},
			},
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "contract")
				require.ErrorContains(t, err, "id")
				require.ErrorContains(t, err, "min")
				require.NotContains(t, err.Error(), `"2"`)
			},
		},
		{
			name:     "JSON_ArrayDocuments_RequiredFieldMissing_ReturnsError",
			payload:  jsonPayload(`[{"_id":"abc"}]`),
			contract: contract("status", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "status"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},
		{
			name:     "JSON_ArrayDocuments_RequiredNestedPathPresent_Passes",
			payload:  jsonPayload(`[{"_id":"abc","address":{"city":"NY"}}]`),
			contract: contract("address.city", true),
			wantRows: []map[string]any{{"_id": "abc", "address": map[string]any{"city": "NY"}}},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "JSON_ArrayDocuments_RequiredNestedPathMissing_ReturnsError",
			payload:  jsonPayload(`[{"_id":"abc","address":{}}]`),
			contract: contract("address.city", true),
			wantErr: func(t *testing.T, err error) {
				require.ErrorContains(t, err, `required field "address.city"`)
				require.ErrorContains(t, err, "runtime payload")
			},
		},
		{
			name:     "JSON_ArrayDocuments_EmptyArray_EmptyRows",
			payload:  jsonPayload(`[]`),
			contract: contract("status", true),
			wantRows: []map[string]any{},
			wantCols: []string{},
			wantErr:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := runtimePayloadToTransformInput(tc.payload, tc.contract)

			tc.wantErr(t, err)

			if tc.wantRows != nil {
				require.NotNil(t, got)
				require.Equal(t, tc.wantRows, got.Rows)
				require.Equal(t, len(tc.wantRows), got.RowCount)
				if tc.wantCols != nil {
					require.Equal(t, tc.wantCols, got.ColumnNames)
				}
			} else {
				require.Nil(t, got)
			}
		})
	}
}
