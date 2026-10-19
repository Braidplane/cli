package payload

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/xerror"
)

//go:generate -command enumer go tool enumer
//go:generate enumer -type=ContractValidationRule -json -text -linecomment -output=contract_validation_rule_enumer.go

// ContractValidationRule identifies a supported field-level contract validation.
type ContractValidationRule int

const (
	ContractValidationRuleUnknown   ContractValidationRule = iota // unknown
	ContractValidationRuleFormat                                  // format
	ContractValidationRuleEnum                                    // enum
	ContractValidationRuleMin                                     // min
	ContractValidationRuleMax                                     // max
	ContractValidationRuleMinLength                               // min_length
	ContractValidationRuleMaxLength                               // max_length
	ContractValidationRuleUnique                                  // unique
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ContractFieldValidationError summarizes a failed field validation without
// carrying payload values into logs, API responses, or persisted diagnostics.
type ContractFieldValidationError struct {
	ContractName   string
	FieldName      string
	Rule           ContractValidationRule
	ViolationCount int
}

func (f *ContractFieldValidationError) Error() string {
	if f == nil {
		return "contract field validation failed"
	}
	return f.Summary()
}

// Summary renders a safe operator-facing validation failure summary.
func (f ContractFieldValidationError) Summary() string {
	contractName := f.ContractName
	if contractName == "" {
		contractName = "contract"
	}
	count := f.ViolationCount
	if count <= 0 {
		count = 1
	}
	if f.Rule == ContractValidationRuleUnknown {
		return fmt.Sprintf(
			"contract %q field %q failed required/non-null validation (%d violation(s))",
			contractName,
			f.FieldName,
			count,
		)
	}
	return fmt.Sprintf(
		"contract %q field %q failed %s validation (%d violation(s))",
		contractName,
		f.FieldName,
		f.Rule.String(),
		count,
	)
}

// ValidateContractFieldDeclarations checks that field validations are syntactically
// valid and compatible with their owning field and contract representation.
func ValidateContractFieldDeclarations(contract *Contract) error {
	if contract == nil {
		return nil
	}

	for _, field := range contract.Fields {
		if err := ValidateDecimalFieldDeclaration(field); err != nil {
			return err
		}
		if err := validateFieldValidationDeclaration(contract, field); err != nil {
			return err
		}
	}
	return nil
}

// CoerceContractFieldValidationRows converts serialized row values into declared
// scalar types for validation-only checks at string-backed boundaries such as CSV.
func CoerceContractFieldValidationRows(
	contract *Contract,
	rows []map[string]any,
) []map[string]any {
	if contract == nil || len(rows) == 0 {
		return rows
	}

	coercedRows := make([]map[string]any, len(rows))
	for i, row := range rows {
		coerced := make(map[string]any, len(row))
		for key, value := range row {
			coerced[key] = value
		}

		for _, field := range contract.Fields {
			value, exists := coerced[field.Name]
			if !exists || value == nil {
				continue
			}
			if converted, ok := coerceSerializedValidationValue(value, field.Type); ok {
				coerced[field.Name] = converted
			}
		}

		coercedRows[i] = coerced
	}
	return coercedRows
}

// ValidateContractFieldValues checks runtime rows against declared field validations
// and returns the first safe failure summary.
func ValidateContractFieldValues(
	contract *Contract,
	rows []map[string]any,
) *ContractFieldValidationError {
	if contract == nil || len(rows) == 0 {
		return nil
	}

	for _, field := range contract.Fields {
		if field.Validations == nil && !field.Required && field.Nullable {
			continue
		}
		if failure := validateRuntimeField(contract, field, rows); failure != nil {
			return failure
		}
	}
	return nil
}

// ContractHasFieldValidations reports whether any field declares a validation
// rule that must be enforced at static or runtime contract boundaries.
func ContractHasFieldValidations(contract *Contract) bool {
	if contract == nil {
		return false
	}
	for _, field := range contract.Fields {
		if hasActiveValidationRule(field.Validations) {
			return true
		}
	}
	return false
}

func validateFieldValidationDeclaration(contract *Contract, field ContractField) error {
	validations := field.Validations
	if validations == nil {
		return nil
	}

	if contract != nil &&
		(contract.Representation == model.ContractRepresentationFile ||
			contract.Representation == model.ContractRepresentationMultipartForm) &&
		hasActiveValidationRule(validations) {
		return invalidValidationDeclaration(field, firstActiveValidationRule(validations), fmt.Sprintf("%s representation does not support field validations", contract.Representation))
	}

	if validations.Format != "" {
		if field.Type != model.ContractFieldTypeValueString {
			return invalidValidationDeclaration(field, ContractValidationRuleFormat, "requires string field type")
		}
		switch validations.Format {
		case "email", "uuid", "url":
		default:
			return invalidValidationDeclaration(field, ContractValidationRuleFormat, fmt.Sprintf("unsupported format %q", validations.Format))
		}
	}

	if validations.Enum != nil {
		if len(validations.Enum) == 0 {
			return invalidValidationDeclaration(field, ContractValidationRuleEnum, "requires at least one literal")
		}
		switch field.Type {
		case model.ContractFieldTypeValueString,
			model.ContractFieldTypeValueInteger,
			model.ContractFieldTypeValueNumber,
			model.ContractFieldTypeValueDecimal,
			model.ContractFieldTypeValueBoolean:
			for _, literal := range validations.Enum {
				if !validationLiteralMatchesFieldType(literal, field.Type) {
					return invalidValidationDeclaration(field, ContractValidationRuleEnum, fmt.Sprintf("literal type is incompatible with %s field", field.Type))
				}
			}
		default:
			return invalidValidationDeclaration(field, ContractValidationRuleEnum, "requires string, number, integer, or boolean field type")
		}
	}

	minValue, hasMin, detail := validationBound(validations.Min, field.Type, ContractValidationRuleMin)
	if detail != "" {
		return invalidValidationDeclaration(field, ContractValidationRuleMin, detail)
	}
	maxValue, hasMax, detail := validationBound(validations.Max, field.Type, ContractValidationRuleMax)
	if detail != "" {
		return invalidValidationDeclaration(field, ContractValidationRuleMax, detail)
	}
	if hasMin || hasMax {
		switch field.Type {
		case model.ContractFieldTypeValueInteger,
			model.ContractFieldTypeValueNumber,
			model.ContractFieldTypeValueDecimal,
			model.ContractFieldTypeValueDatetime:
		default:
			return invalidValidationDeclaration(field, ContractValidationRuleMin, "min/max require number, integer, or datetime field type")
		}
		if hasMin && hasMax && compareComparable(minValue, maxValue) > 0 {
			return invalidValidationDeclaration(field, ContractValidationRuleMin, "min must be less than or equal to max")
		}
	}

	if validations.MinLength != nil || validations.MaxLength != nil {
		switch field.Type {
		case model.ContractFieldTypeValueString, model.ContractFieldTypeValueArray:
		default:
			return invalidValidationDeclaration(field, ContractValidationRuleMinLength, "length validations require string or array field type")
		}
		if validations.MinLength != nil && *validations.MinLength < 0 {
			return invalidValidationDeclaration(field, ContractValidationRuleMinLength, "must be non-negative")
		}
		if validations.MaxLength != nil && *validations.MaxLength < 0 {
			return invalidValidationDeclaration(field, ContractValidationRuleMaxLength, "must be non-negative")
		}
		if validations.MinLength != nil &&
			validations.MaxLength != nil &&
			*validations.MinLength > *validations.MaxLength {
			return invalidValidationDeclaration(field, ContractValidationRuleMinLength, "min_length must be less than or equal to max_length")
		}
	}

	if validations.Unique != nil && *validations.Unique {
		representation := contract.Representation
		if representation != model.ContractRepresentationUnknown &&
			representation != model.ContractRepresentationRows &&
			representation != model.ContractRepresentationCSV {
			return invalidValidationDeclaration(field, ContractValidationRuleUnique, fmt.Sprintf("requires rows or csv representation, got %s", contract.Representation))
		}
		switch field.Type {
		case model.ContractFieldTypeValueObject, model.ContractFieldTypeValueArray:
			return invalidValidationDeclaration(field, ContractValidationRuleUnique, "requires a scalar field type")
		default:
		}
	}

	return nil
}

// coerceSerializedValidationValue parses string-backed scalar values into the
// declared field type so validation rules can run against serialized rows.
func coerceSerializedValidationValue(value any, fieldType model.ContractFieldType) (any, bool) {
	text, ok := value.(string)
	if !ok {
		return value, true
	}
	if text == "" {
		return value, true
	}

	switch fieldType {
	case model.ContractFieldTypeValueInteger:
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return value, true
		}
		return parsed, true
	case model.ContractFieldTypeValueDecimal:
		parsed, err := decimal.From(value)
		if err != nil {
			return value, true
		}
		return parsed, true
	case model.ContractFieldTypeValueNumber:
		parsed, ok := numberValue(json.Number(text))
		if !ok {
			return value, true
		}
		return parsed, true
	case model.ContractFieldTypeValueBoolean:
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return value, true
		}
		return parsed, true
	default:
		return value, true
	}
}

func invalidValidationDeclaration(field ContractField, rule ContractValidationRule, detail string) error {
	return xerror.InvalidInput(xerror.Pipeline).Detailf(
		"field %q validation %q is invalid: %s",
		field.Name,
		rule.String(),
		detail,
	)
}

func validateRuntimeField(
	contract *Contract,
	field ContractField,
	rows []map[string]any,
) *ContractFieldValidationError {
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		resolved, exists, hasMissing, err := resolveRuntimeValidationFieldValues(row, field.Name)
		if err != nil {
			return contractFieldValidationFailure(
				contract,
				field,
				structuralValidationRule(field.Validations),
				1,
			)
		}
		if !exists {
			if field.Required {
				return contractFieldValidationFailure(
					contract,
					field,
					structuralValidationRule(field.Validations),
					1,
				)
			}
			continue
		}
		if hasMissing && (field.Required || hasActiveValidationRule(field.Validations)) {
			return contractFieldValidationFailure(
				contract,
				field,
				structuralValidationRule(field.Validations),
				1,
			)
		}
		for _, value := range resolved {
			if value == nil {
				if field.Nullable {
					continue
				}
				return contractFieldValidationFailure(
					contract,
					field,
					structuralValidationRule(field.Validations),
					1,
				)
			}
			values = append(values, value)
		}
	}
	if !hasActiveValidationRule(field.Validations) {
		return nil
	}
	if len(values) == 0 {
		return nil
	}

	checks := []struct {
		rule  ContractValidationRule
		count func() int
	}{
		{ContractValidationRuleFormat, func() int { return countFormatViolations(field.Validations.Format, values) }},
		{ContractValidationRuleEnum, func() int { return countEnumViolations(field.Type, field.Validations.Enum, values) }},
		{ContractValidationRuleMin, func() int { return countMinViolations(field.Type, field.Validations.Min, values) }},
		{ContractValidationRuleMax, func() int { return countMaxViolations(field.Type, field.Validations.Max, values) }},
		{ContractValidationRuleMinLength, func() int { return countMinLengthViolations(field.Validations.MinLength, values) }},
		{ContractValidationRuleMaxLength, func() int { return countMaxLengthViolations(field.Validations.MaxLength, values) }},
		{ContractValidationRuleUnique, func() int { return countUniqueViolations(field.Type, field.Validations.Unique, values) }},
	}
	for _, check := range checks {
		count := check.count()
		if count > 0 {
			return contractFieldValidationFailure(contract, field, check.rule, count)
		}
	}
	return nil
}

func contractFieldValidationFailure(
	contract *Contract,
	field ContractField,
	rule ContractValidationRule,
	count int,
) *ContractFieldValidationError {
	contractName := ""
	if contract != nil {
		contractName = contract.Name
	}
	return &ContractFieldValidationError{
		ContractName:   contractName,
		FieldName:      field.Name,
		Rule:           rule,
		ViolationCount: count,
	}
}

func resolveRuntimeValidationFieldValues(
	row map[string]any,
	fieldName string,
) ([]any, bool, bool, error) {
	if value, ok := row[fieldName]; ok {
		return []any{value}, true, false, nil
	}

	return ResolveFieldPathValues(row, fieldName)
}

func firstActiveValidationRule(validations *ContractFieldValidations) ContractValidationRule {
	if validations == nil {
		return ContractValidationRuleEnum
	}
	if validations.Format != "" {
		return ContractValidationRuleFormat
	}
	if validations.Enum != nil {
		return ContractValidationRuleEnum
	}
	if validations.Min != nil {
		return ContractValidationRuleMin
	}
	if validations.Max != nil {
		return ContractValidationRuleMax
	}
	if validations.MinLength != nil {
		return ContractValidationRuleMinLength
	}
	if validations.MaxLength != nil {
		return ContractValidationRuleMaxLength
	}
	if validations.Unique != nil && *validations.Unique {
		return ContractValidationRuleUnique
	}
	return ContractValidationRuleEnum
}

func structuralValidationRule(validations *ContractFieldValidations) ContractValidationRule {
	if !hasActiveValidationRule(validations) {
		return ContractValidationRuleUnknown
	}
	return firstActiveValidationRule(validations)
}

func hasActiveValidationRule(validations *ContractFieldValidations) bool {
	if validations == nil {
		return false
	}
	return validations.Format != "" ||
		validations.Enum != nil ||
		validations.Min != nil ||
		validations.Max != nil ||
		validations.MinLength != nil ||
		validations.MaxLength != nil ||
		(validations.Unique != nil && *validations.Unique)
}

func validationLiteralMatchesFieldType(value any, fieldType model.ContractFieldType) bool {
	switch fieldType {
	case model.ContractFieldTypeValueString:
		_, ok := value.(string)
		return ok
	case model.ContractFieldTypeValueBoolean:
		_, ok := value.(bool)
		return ok
	case model.ContractFieldTypeValueInteger:
		_, ok := integerValue(value)
		return ok
	case model.ContractFieldTypeValueDecimal:
		_, err := decimal.From(value)
		return err == nil
	case model.ContractFieldTypeValueNumber:
		_, ok := numberValue(value)
		return ok
	default:
		return false
	}
}

func validationBound(value any, fieldType model.ContractFieldType, rule ContractValidationRule) (any, bool, string) {
	if value == nil {
		return nil, false, ""
	}
	ruleName := rule.String()
	switch fieldType {
	case model.ContractFieldTypeValueInteger:
		bound, ok := integerValue(value)
		if !ok {
			return nil, false, ruleName + " requires an integer bound"
		}
		return bound, true, ""
	case model.ContractFieldTypeValueDecimal:
		bound, err := decimal.From(value)
		if err != nil {
			return nil, false, ruleName + " requires an exact decimal bound"
		}
		return bound, true, ""
	case model.ContractFieldTypeValueNumber:
		bound, ok := numberValue(value)
		if !ok {
			return nil, false, ruleName + " requires a finite numeric bound with lossless integer conversion"
		}
		return bound, true, ""
	case model.ContractFieldTypeValueDatetime:
		text, ok := value.(string)
		if !ok {
			return nil, false, ruleName + " requires an RFC3339 datetime string bound"
		}
		bound, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, false, ruleName + " requires an RFC3339 datetime string bound"
		}
		return bound, true, ""
	default:
		return nil, true, ""
	}
}

func compareComparable(left, right any) int {
	switch l := left.(type) {
	case decimal.Value:
		return l.Cmp(right.(decimal.Value))
	case int64:
		r := right.(int64)
		return compareInt64(l, r)
	case float64:
		r := right.(float64)
		return compareFloat64(l, r)
	case time.Time:
		r := right.(time.Time)
		switch {
		case l.Before(r):
			return -1
		case l.After(r):
			return 1
		default:
			return 0
		}
	default:
		return 0
	}
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareFloat64(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func countFormatViolations(format string, values []any) int {
	if format == "" {
		return 0
	}
	count := 0
	for _, value := range values {
		text, ok := value.(string)
		if !ok || !matchesFormat(text, format) {
			count++
		}
	}
	return count
}

func matchesFormat(value, format string) bool {
	switch format {
	case "email":
		parsed, err := mail.ParseAddress(value)
		return err == nil && parsed.Address == value
	case "uuid":
		return uuidPattern.MatchString(value)
	case "url":
		parsed, err := url.Parse(value)
		return err == nil && parsed.Scheme != "" && parsed.Host != ""
	default:
		return false
	}
}

func countEnumViolations(fieldType model.ContractFieldType, allowed, values []any) int {
	if allowed == nil {
		return 0
	}
	count := 0
	for _, value := range values {
		if !enumContainsValue(fieldType, allowed, value) {
			count++
		}
	}
	return count
}

func enumContainsValue(fieldType model.ContractFieldType, allowed []any, value any) bool {
	for _, candidate := range allowed {
		if validationValuesEqual(fieldType, candidate, value) {
			return true
		}
	}
	return false
}

func validationValuesEqual(fieldType model.ContractFieldType, left, right any) bool {
	switch fieldType {
	case model.ContractFieldTypeValueString:
		l, lok := left.(string)
		r, rok := right.(string)
		return lok && rok && l == r
	case model.ContractFieldTypeValueBoolean:
		l, lok := left.(bool)
		r, rok := right.(bool)
		return lok && rok && l == r
	case model.ContractFieldTypeValueInteger:
		l, lok := integerValue(left)
		r, rok := integerValue(right)
		return lok && rok && l == r
	case model.ContractFieldTypeValueDecimal:
		l, le := decimal.From(left)
		r, re := decimal.From(right)
		return le == nil && re == nil && l.Cmp(r) == 0
	case model.ContractFieldTypeValueNumber:
		l, lok := numberValue(left)
		r, rok := numberValue(right)
		return lok && rok && l == r
	default:
		return false
	}
}

func countMinViolations(fieldType model.ContractFieldType, minimum any, values []any) int {
	bound, ok, _ := validationBound(minimum, fieldType, ContractValidationRuleMin)
	if !ok {
		return 0
	}
	return countBoundViolations(fieldType, values, bound, func(order int) bool { return order < 0 })
}

func countMaxViolations(fieldType model.ContractFieldType, maximum any, values []any) int {
	bound, ok, _ := validationBound(maximum, fieldType, ContractValidationRuleMax)
	if !ok {
		return 0
	}
	return countBoundViolations(fieldType, values, bound, func(order int) bool { return order > 0 })
}

func countBoundViolations(
	fieldType model.ContractFieldType,
	values []any,
	bound any,
	violates func(int) bool,
) int {
	count := 0
	for _, value := range values {
		comparableValue, ok := comparableRuntimeValue(value, fieldType)
		if !ok || violates(compareComparable(comparableValue, bound)) {
			count++
		}
	}
	return count
}

func comparableRuntimeValue(value any, fieldType model.ContractFieldType) (any, bool) {
	switch fieldType {
	case model.ContractFieldTypeValueInteger:
		return integerValue(value)
	case model.ContractFieldTypeValueDecimal:
		parsed, err := decimal.From(value)
		return parsed, err == nil
	case model.ContractFieldTypeValueNumber:
		return numberValue(value)
	case model.ContractFieldTypeValueDatetime:
		if typed, ok := value.(time.Time); ok {
			return typed, true
		}
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		parsed, err := time.Parse(time.RFC3339, text)
		return parsed, err == nil
	default:
		return nil, false
	}
}

func countMinLengthViolations(minLength *int, values []any) int {
	if minLength == nil {
		return 0
	}
	count := 0
	for _, value := range values {
		length, ok := validationLength(value)
		if !ok || length < *minLength {
			count++
		}
	}
	return count
}

func countMaxLengthViolations(maxLength *int, values []any) int {
	if maxLength == nil {
		return 0
	}
	count := 0
	for _, value := range values {
		length, ok := validationLength(value)
		if !ok || length > *maxLength {
			count++
		}
	}
	return count
}

func validationLength(value any) (int, bool) {
	switch typed := value.(type) {
	case string:
		return utf8.RuneCountInString(typed), true
	case []any:
		return len(typed), true
	default:
		reflected := reflect.ValueOf(value)
		switch reflected.Kind() {
		case reflect.Array, reflect.Slice:
			return reflected.Len(), true
		default:
		}
		return 0, false
	}
}

func countUniqueViolations(fieldType model.ContractFieldType, unique *bool, values []any) int {
	if unique == nil || !*unique {
		return 0
	}
	seen := make(map[string]struct{}, len(values))
	count := 0
	for _, value := range values {
		key, ok := uniqueKey(fieldType, value)
		if !ok {
			count++
			continue
		}
		if _, exists := seen[key]; exists {
			count++
			continue
		}
		seen[key] = struct{}{}
	}
	return count
}

// uniqueKey builds a stable comparable key using the declared field type so
// integer precision and datetime values are preserved across runtime sources.
func uniqueKey(fieldType model.ContractFieldType, value any) (string, bool) {
	switch fieldType {
	case model.ContractFieldTypeValueString:
		typed, ok := value.(string)
		if !ok {
			return "", false
		}
		return "s:" + typed, true
	case model.ContractFieldTypeValueBoolean:
		typed, ok := value.(bool)
		if !ok {
			return "", false
		}
		return "b:" + strconv.FormatBool(typed), true
	case model.ContractFieldTypeValueInteger:
		typed, ok := integerValue(value)
		if !ok {
			return "", false
		}
		return "i:" + strconv.FormatInt(typed, 10), true
	case model.ContractFieldTypeValueDecimal:
		typed, err := decimal.From(value)
		if err != nil {
			return "", false
		}
		return "d:" + typed.Rat().RatString(), true
	case model.ContractFieldTypeValueNumber:
		typed, ok := numberValue(value)
		if !ok {
			return "", false
		}
		return "n:" + strconv.FormatFloat(typed, 'g', -1, 64), true
	case model.ContractFieldTypeValueDatetime:
		typed, ok := comparableRuntimeValue(value, model.ContractFieldTypeValueDatetime)
		if !ok {
			return "", false
		}
		return "t:" + typed.(time.Time).UTC().Format(time.RFC3339Nano), true
	default:
		return genericUniqueKey(value)
	}
}

// genericUniqueKey preserves legacy scalar uniqueness behavior for fields whose
// declaration type is not specific enough to choose a typed key.
func genericUniqueKey(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return "s:" + typed, true
	case bool:
		return "b:" + strconv.FormatBool(typed), true
	case json.Number:
		return "n:" + typed.String(), true
	case int:
		return "i:" + strconv.FormatInt(int64(typed), 10), true
	case int8:
		return "i:" + strconv.FormatInt(int64(typed), 10), true
	case int16:
		return "i:" + strconv.FormatInt(int64(typed), 10), true
	case int32:
		return "i:" + strconv.FormatInt(int64(typed), 10), true
	case int64:
		return "i:" + strconv.FormatInt(typed, 10), true
	case uint:
		return "u:" + strconv.FormatUint(uint64(typed), 10), true
	case uint8:
		return "u:" + strconv.FormatUint(uint64(typed), 10), true
	case uint16:
		return "u:" + strconv.FormatUint(uint64(typed), 10), true
	case uint32:
		return "u:" + strconv.FormatUint(uint64(typed), 10), true
	case uint64:
		return "u:" + strconv.FormatUint(typed, 10), true
	case float32, float64:
		number, ok := numberValue(typed)
		if !ok {
			return "", false
		}
		return "n:" + strconv.FormatFloat(number, 'g', -1, 64), true
	case time.Time:
		return "t:" + typed.UTC().Format(time.RFC3339Nano), true
	default:
		return "", false
	}
}

func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > math.MaxInt64 {
			return 0, false
		}
		return int64(typed), true
	case float32:
		value := float64(typed)
		if !finiteFloat(value) || value < math.MinInt64 || value >= 0x1p63 || math.Trunc(value) != value {
			return 0, false
		}
		return int64(value), true
	case float64:
		if !finiteFloat(typed) || typed < math.MinInt64 || typed >= 0x1p63 || math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return numberValue(json.Number(fmt.Sprint(typed)))
	case float32:
		value := float64(typed)
		return value, finiteFloat(value)
	case float64:
		return typed, finiteFloat(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil || !finiteFloat(parsed) {
			return 0, false
		}
		if math.Abs(parsed) >= 0x1p53 {
			// Bound exact parsing; small values need no integer precision check.
			if len(typed) > 128 {
				return 0, false
			}
			exact, ok := new(big.Rat).SetString(typed.String())
			if !ok || (exact.IsInt() && exact.Cmp(new(big.Rat).SetFloat64(parsed)) != 0) {
				return 0, false
			}
		}
		return parsed, true
	default:
		return 0, false
	}
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
