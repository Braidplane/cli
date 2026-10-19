package payload

import (
	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/xerror"
)

func ValidateDecimalFieldDeclaration(field ContractField) error {
	if field.Type != model.ContractFieldTypeValueDecimal {
		if field.Precision != nil || field.Scale != nil {
			return decimal.ErrConversion.Detail("precision and scale require decimal field type")
		}
		return nil
	}
	if field.Precision == nil || field.Scale == nil {
		return decimal.ErrConversion.Detail("decimal fields require precision and scale")
	}
	if err := decimal.ValidateDeclaration(*field.Precision, *field.Scale); err != nil {
		return xerror.Wrapf("validate decimal declaration: %w", err)
	}
	return nil
}

// DecimalFieldCompatible proves that every producer value fits without rounding.
func DecimalFieldCompatible(producer, consumer ContractField) bool {
	if ValidateDecimalFieldDeclaration(producer) != nil || ValidateDecimalFieldDeclaration(consumer) != nil || producer.Type != model.ContractFieldTypeValueDecimal || consumer.Type != model.ContractFieldTypeValueDecimal {
		return false
	}
	return *producer.Scale <= *consumer.Scale && *producer.Precision-*producer.Scale <= *consumer.Precision-*consumer.Scale
}

// DecimalFieldValue validates exact representability; rounding belongs in an explicit cast.
func DecimalFieldValue(value any, field ContractField) (decimal.Value, error) {
	if field.Type != model.ContractFieldTypeValueDecimal {
		return decimal.Value{}, decimal.ErrConversion.Detail("decimal field type required")
	}
	if err := ValidateDecimalFieldDeclaration(field); err != nil {
		return decimal.Value{}, xerror.Wrapf("bind decimal field %q: %w", field.Name, err)
	}
	parsed, err := decimal.From(value)
	if err != nil {
		return decimal.Value{}, xerror.Wrapf("bind decimal field %q: %w", field.Name, err)
	}
	converted, err := parsed.Quantize(*field.Precision, *field.Scale)
	if err != nil {
		return decimal.Value{}, xerror.Wrapf("bind decimal field %q: %w", field.Name, err)
	}
	if converted.Cmp(parsed) != 0 {
		return decimal.Value{}, decimal.ErrConversion.Detail("value requires rounding to the declared decimal scale; use an explicit cast")
	}
	return converted, nil
}

// CoerceDecimalRows binds declared decimals without changing other scalar semantics.
func CoerceDecimalRows(contract *Contract, rows []map[string]any) ([]map[string]any, error) {
	if contract == nil {
		return rows, nil
	}
	result := rows
	for _, field := range contract.Fields {
		if err := ValidateDecimalFieldDeclaration(field); err != nil {
			return nil, err
		}
		if field.Type != model.ContractFieldTypeValueDecimal {
			continue
		}
		convertedRows := make([]map[string]any, len(result))
		for i, row := range result {
			converted, err := coerceDecimalPath(row, parseFieldPath(field.Name), field)
			if err != nil {
				return nil, err
			}
			convertedRows[i] = converted.(map[string]any)
		}
		result = convertedRows
	}
	return result, nil
}

func coerceDecimalPath(node any, path []fieldPathSegment, field ContractField) (any, error) {
	if len(path) == 0 {
		if node == nil {
			if !field.Nullable {
				return nil, decimal.ErrConversion.Detail("non-nullable decimal field contains null")
			}
			return nil, nil //nolint:nilnil // Nullable decimal value.
		}
		return DecimalFieldValue(node, field)
	}
	object, ok := node.(map[string]any)
	if !ok {
		return nil, decimal.ErrConversion.Detail("decimal field path requires an object")
	}
	segment := path[0]
	value, exists := object[segment.key]
	if !exists {
		if field.Required {
			return nil, decimal.ErrConversion.Detail("required decimal field is missing")
		}
		return node, nil
	}
	result := cloneMapAny(object)
	if segment.isArray {
		array, ok := value.([]any)
		if !ok {
			return nil, decimal.ErrConversion.Detail("decimal field path requires an array")
		}
		converted := make([]any, len(array))
		for i, item := range array {
			var err error
			converted[i], err = coerceDecimalPath(item, path[1:], field)
			if err != nil {
				return nil, err
			}
		}
		result[segment.key] = converted
	} else {
		converted, err := coerceDecimalPath(value, path[1:], field)
		if err != nil {
			return nil, err
		}
		result[segment.key] = converted
	}
	return result, nil
}
