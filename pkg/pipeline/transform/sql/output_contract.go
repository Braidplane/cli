package sql

import (
	"encoding/json"

	"github.com/Braidplane/cli/pkg/model"
	pipepayload "github.com/Braidplane/cli/pkg/pipeline/payload"
	"github.com/Braidplane/cli/pkg/xerror"
)

// ValidateOutputContract checks runtime transform output against the declared contract.
// It dispatches by output kind so each representation can enforce shape rules consistently,
// and fails closed on unknown kinds to prevent silent validation gaps.
// Returns ErrOutputContractViolation (wrapped) on the first detected violation.
func ValidateOutputContract(
	output *pipepayload.Output,
	contract *pipepayload.Contract,
) error {
	switch output.OutputKind {
	case model.TransformOutputKindRows:
		return validateRowsOutputContract(output.Rows, contract)
	case model.TransformOutputKindJSON:
		return validateDocumentOutputContract(output.Document, contract)
	case model.TransformOutputKindCSV:
		// CSV is serialized text; all values are strings. Type validation is not applicable.
		// Field validations run when serialized row data is available.
		return validateCSVOutputContract(output.ColumnNames, output.CSV, contract)
	default:
		// Fail closed: unknown output kinds indicate missing validation logic,
		// not a valid output kind with deferred validation.
		return xerror.Wrapf(
			"unsupported output kind for contract validation: %v: %w",
			output.OutputKind,
			ErrOutputContractViolation,
		)
	}
}

// validateRowsOutputContract validates row outputs field-by-field, including nested paths
// and array element paths, because runtime SQL evaluation can produce structural drift that
// static SQL projection checks alone cannot guarantee.
func validateRowsOutputContract(rows []map[string]any, contract *pipepayload.Contract) error {
	for rowIdx, row := range rows {
		for _, field := range contract.Fields {
			vals, exists, hasMissing, err := pipepayload.ResolveFieldPathValues(row, field.Name)
			if err != nil {
				return xerror.Wrapf("row %d: %w", rowIdx, xerror.Join(ErrOutputContractViolation, err))
			}
			if !exists || hasMissing {
				if field.Required {
					return xerror.Wrapf(
						"row %d: required field %q is missing: %w",
						rowIdx,
						field.Name,
						ErrOutputContractViolation,
					)
				}
				continue
			}
			for _, val := range vals {
				if val == nil {
					if !field.Nullable {
						return xerror.Wrapf(
							"row %d: field %q is non-nullable but got null: %w",
							rowIdx,
							field.Name,
							ErrOutputContractViolation,
						)
					}
					continue
				}
				if field.Type == model.ContractFieldTypeValueDecimal {
					if _, err := pipepayload.DecimalFieldValue(val, field); err != nil {
						return xerror.Wrapf("decimal output field %q: %w", field.Name, xerror.Join(ErrOutputContractViolation, err))
					}
					continue
				}
				if err := checkFieldType(field.Name, val, field.Type); err != nil {
					return xerror.Wrapf("row %d: %w", rowIdx, err)
				}
			}
		}
	}
	if failure := pipepayload.ValidateContractFieldValues(contract, rows); failure != nil {
		return xerror.Wrapf(
			"%s: %w",
			failure.Summary(),
			xerror.Join(ErrOutputContractViolation, failure),
		)
	}
	return nil
}

// validateDocumentOutputContract validates JSON outputs against the same contract
// semantics as row outputs so JSON mode remains behaviorally consistent with rows mode.
// JSON arrays are validated row-by-row; JSON objects are treated as one row.
func validateDocumentOutputContract(doc json.RawMessage, contract *pipepayload.Contract) error {
	if len(doc) == 0 {
		for _, field := range contract.Fields {
			if field.Required {
				return xerror.Wrapf(
					"document is empty: required field %q is missing: %w",
					field.Name,
					ErrOutputContractViolation,
				)
			}
		}
		return nil
	}

	if contract.RootShape != model.ContractRootShapeUnknown {
		rootShape := pipepayload.DetectJSONRootShape(doc)
		if rootShape != contract.RootShape {
			return xerror.Wrapf(
				"document: root_shape expects %s but got %s: %w",
				contract.RootShape,
				rootShape,
				ErrOutputContractViolation,
			)
		}
	}

	rows, _, err := pipepayload.ParseJSONBodyToRowsPreserveNumbers(doc)
	if err != nil {
		return xerror.Wrapf("document: %w", xerror.Join(ErrOutputContractViolation, err))
	}

	return validateRowsOutputContract(rows, contract)
}

// validateCSVOutputContract enforces required column presence and value validations for
// CSV output. Type and nullability checks are intentionally skipped because CSV runtime
// values are serialized strings.
func validateCSVOutputContract(columnNames []string, csvBody string, contract *pipepayload.Contract) error {
	columnSet := make(map[string]struct{}, len(columnNames))
	for _, name := range columnNames {
		columnSet[name] = struct{}{}
	}

	for _, field := range contract.Fields {
		if _, exists := columnSet[field.Name]; !exists && field.Required {
			return xerror.Wrapf(
				"csv: required field %q is missing from column names: %w",
				field.Name,
				ErrOutputContractViolation,
			)
		}
	}
	if csvBody == "" {
		return nil
	}
	rows, _, err := pipepayload.ParseCSVBodyToRows([]byte(csvBody))
	if err != nil {
		return xerror.Wrapf("csv: %w", xerror.Join(ErrOutputContractViolation, err))
	}
	rows, err = pipepayload.CoerceDecimalRows(contract, rows)
	if err != nil {
		return xerror.Wrapf("decimal CSV output: %w", xerror.Join(ErrOutputContractViolation, err))
	}
	rows = pipepayload.CoerceContractFieldValidationRows(contract, rows)
	if failure := pipepayload.ValidateContractFieldValues(contract, rows); failure != nil {
		return xerror.Wrapf(
			"%s: %w",
			failure.Summary(),
			xerror.Join(ErrOutputContractViolation, failure),
		)
	}
	return nil
}

// checkFieldType infers the runtime type of val and compares it to the declared type using
// the same compatibility rules as the static validator (integer ⊂ number).
func checkFieldType(fieldName string, val any, declared model.ContractFieldType) error {
	if declared == model.ContractFieldTypeUnknown {
		return nil
	}
	inferred := pipepayload.InferFieldType(val)
	if inferred == model.ContractFieldTypeUnknown {
		// nil / untyped values are not validated against type.
		return nil
	}
	if !isCompatibleContractFieldType(inferred, declared) {
		return xerror.Wrapf(
			"field %q expects %s but got %s: %w",
			fieldName,
			declared,
			inferred,
			ErrOutputContractViolation,
		)
	}
	return nil
}
