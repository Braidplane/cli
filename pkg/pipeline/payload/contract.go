package payload

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
	formpayload "github.com/Braidplane/cli/pkg/payload/form"
	"github.com/Braidplane/cli/pkg/xerror"
)

type fieldPathSegment struct {
	key      string
	isArray  bool
	rawToken string
}

func parseFieldPath(path string) []fieldPathSegment {
	parts := strings.Split(path, ".")
	segments := make([]fieldPathSegment, 0, len(parts))
	for _, part := range parts {
		seg := fieldPathSegment{rawToken: part}
		if strings.HasSuffix(part, "[]") {
			seg.key = strings.TrimSuffix(part, "[]")
			seg.isArray = true
		} else {
			seg.key = part
		}
		segments = append(segments, seg)
	}
	return segments
}

// ResolveFieldPathValues walks a field path through nested maps/arrays and returns
// all matched terminal values. It also reports whether the path existed and
// whether any branch had missing segments.
func ResolveFieldPathValues(
	root map[string]any,
	fieldPath string,
) ([]any, bool, bool, error) {
	segments := parseFieldPath(fieldPath)
	current := []any{root}
	pathExists := false
	hasMissing := false

	for _, segment := range segments {
		next := make([]any, 0)
		segmentFound := false

		for _, node := range current {
			obj, ok := node.(map[string]any)
			if !ok {
				return nil, false, false, xerror.Wrapf(
					"field %q cannot be resolved: expected object at segment %q: %w",
					fieldPath,
					segment.rawToken,
					xerror.InvalidFieldPath(),
				)
			}

			val, exists := obj[segment.key]
			if !exists {
				hasMissing = true
				continue
			}
			segmentFound = true

			if segment.isArray {
				arr, ok := val.([]any)
				if !ok {
					return nil, false, false, xerror.Wrapf(
						"field %q expects array at segment %q but got %T: %w",
						fieldPath,
						segment.rawToken,
						val,
						xerror.InvalidFieldPath(),
					)
				}
				next = append(next, arr...)
				continue
			}

			next = append(next, val)
		}

		if segmentFound {
			pathExists = true
		}
		if !segmentFound {
			return nil, false, hasMissing, nil
		}
		current = next
	}

	return current, pathExists, hasMissing, nil
}

// FieldPathExists returns true when at least one row can resolve the full field
// path without missing any branch in that row.
func FieldPathExists(rows []map[string]any, fieldPath string) bool {
	for _, row := range rows {
		_, exists, hasMissing, err := ResolveFieldPathValues(row, fieldPath)
		if err != nil {
			continue
		}
		if exists && !hasMissing {
			return true
		}
	}
	return false
}

// InferFieldType maps a Go runtime value to the nearest ContractFieldType.
func InferFieldType(val any) model.ContractFieldType {
	if val == nil {
		return model.ContractFieldTypeUnknown
	}
	switch v := val.(type) {
	case decimal.Value:
		return model.ContractFieldTypeValueDecimal
	case string:
		return model.ContractFieldTypeValueString
	case bool:
		return model.ContractFieldTypeValueBoolean
	case map[string]any:
		return model.ContractFieldTypeValueObject
	case []any:
		return model.ContractFieldTypeValueArray
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return model.ContractFieldTypeValueInteger
	case float32, float64:
		return model.ContractFieldTypeValueNumber
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return model.ContractFieldTypeValueInteger
		}
		return model.ContractFieldTypeValueNumber
	default:
		return model.ContractFieldTypeUnknown
	}
}

func CollectColumnNames(rows []map[string]any) []string {
	if len(rows) == 0 {
		return []string{}
	}

	columnSet := make(map[string]struct{})
	for _, row := range rows {
		for columnName := range row {
			columnSet[columnName] = struct{}{}
		}
	}

	columnNames := make([]string, 0, len(columnSet))
	for columnName := range columnSet {
		columnNames = append(columnNames, columnName)
	}
	sort.Strings(columnNames)

	return columnNames
}

// ParseJSONBodyToRows decodes a JSON body (array or object) into rows.
// Arrays produce one row per element; a top-level object produces a single row.
// Scalars, null, and malformed JSON are rejected as unsupported.
func ParseJSONBodyToRows(body []byte) ([]map[string]any, []string, error) {
	return parseJSONBodyToRows(body, false)
}

// ParseJSONBodyToRowsPreserveNumbers decodes JSON rows while retaining
// json.Number values so callers can distinguish integer and decimal literals.
func ParseJSONBodyToRowsPreserveNumbers(body []byte) ([]map[string]any, []string, error) {
	return parseJSONBodyToRows(body, true)
}

// DetectJSONRootShape reports whether a JSON payload is a top-level object or array.
// Unknown is returned for empty, malformed, scalar, or null payloads.
func DetectJSONRootShape(body []byte) model.ContractRootShape {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return model.ContractRootShapeUnknown
	}
	switch trimmed[0] {
	case '{':
		return model.ContractRootShapeObject
	case '[':
		return model.ContractRootShapeArray
	default:
		return model.ContractRootShapeUnknown
	}
}

// parseJSONBodyToRows decodes a JSON body into row objects while keeping the
// number handling choice local to payload normalization.
func parseJSONBodyToRows(
	body []byte,
	preserveNumbers bool,
) ([]map[string]any, []string, error) {
	var decoded any
	if preserveNumbers {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, nil, xerror.UnsupportedHTTPResponseFormat()
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, nil, xerror.UnsupportedHTTPResponseFormat()
		}
	} else {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, nil, xerror.UnsupportedHTTPResponseFormat()
		}
	}

	var rows []map[string]any
	switch v := decoded.(type) {
	case []any:
		rows = make([]map[string]any, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, nil, xerror.UnsupportedHTTPResponseFormat()
			}
			rows = append(rows, obj)
		}
	case map[string]any:
		rows = []map[string]any{v}
	default:
		return nil, nil, xerror.UnsupportedHTTPResponseFormat()
	}

	return rows, CollectColumnNames(rows), nil
}

// ParseCSVBodyToRows decodes a CSV body into rows. The first row is treated as
// a header. Values are always strings.
func ParseCSVBodyToRows(body []byte) ([]map[string]any, []string, error) {
	r := csv.NewReader(bytes.NewReader(body))
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, nil, xerror.UnsupportedHTTPResponseFormat()
	}

	headers := records[0]
	seen := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		if strings.TrimSpace(header) == "" {
			return nil, nil, xerror.UnsupportedHTTPResponseFormat()
		}
		if _, exists := seen[header]; exists {
			return nil, nil, xerror.UnsupportedHTTPResponseFormat()
		}
		seen[header] = struct{}{}
	}

	rows := make([]map[string]any, 0, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]any, len(headers))
		for i, h := range headers {
			if i < len(record) {
				row[h] = record[i]
			}
		}
		rows = append(rows, row)
	}

	return rows, headers, nil
}

func EffectiveContract(
	name string,
	payload *Payload,
	declared *Contract,
) *Contract {
	if payload == nil {
		return nil
	}

	contract := CloneContract(declared)
	if contract == nil {
		contract = &Contract{}
	}

	contract.Name = name
	if contract.Representation == model.ContractRepresentationUnknown {
		contract.Representation = payload.Representation
	}
	if contract.RootShape == model.ContractRootShapeUnknown {
		contract.RootShape = payload.RootShape
	}
	if len(contract.Fields) == 0 {
		contract.Fields = DeriveContractFields(payload)
	}

	return contract
}

func DeriveContractFields(payload *Payload) []ContractField {
	if payload != nil && payload.Representation == model.ContractRepresentationMultipartForm {
		return deriveFormContractFields(payload.Form)
	}

	columnNames, rows, err := ContractData(payload)
	if err != nil {
		return nil
	}
	if len(columnNames) == 0 {
		columnNames = CollectColumnNames(rows)
	}
	if len(columnNames) == 0 {
		return nil
	}

	fields := make([]ContractField, 0, len(columnNames))
	for _, fieldName := range columnNames {
		fieldType, nullable := InferContractField(rows, fieldName)
		fields = append(fields, ContractField{
			Name:     fieldName,
			Type:     fieldType,
			Required: false,
			Nullable: nullable,
		})
	}

	return fields
}

func ContractData(payload *Payload) ([]string, []map[string]any, error) {
	if payload == nil {
		return nil, nil, nil
	}

	switch payload.Representation {
	case model.ContractRepresentationRows:
		return append([]string(nil), payload.ColumnNames...), payload.Rows, nil
	case model.ContractRepresentationJSON:
		if len(payload.Document) == 0 {
			return nil, nil, nil
		}
		rows, columnNames, err := ParseJSONBodyToRowsPreserveNumbers(payload.Document)
		return columnNames, rows, err
	case model.ContractRepresentationCSV:
		if payload.CSV == "" {
			return nil, nil, nil
		}
		rows, columnNames, err := ParseCSVBodyToRows([]byte(payload.CSV))
		return columnNames, rows, err
	case model.ContractRepresentationMultipartForm:
		columnNames, rows := formpayload.ContractData(payload.Form)
		return columnNames, rows, nil
	default:
		return nil, nil, nil
	}
}

// deriveFormContractFields maps form-specific summaries into pipeline contract fields.
func deriveFormContractFields(form *formpayload.FormData) []ContractField {
	formFields := formpayload.ContractFields(form)
	if len(formFields) == 0 {
		return nil
	}

	fields := make([]ContractField, 0, len(formFields))
	for _, field := range formFields {
		fields = append(fields, ContractField{
			Name: field.Name,
			Type: field.Type,
		})
	}
	return fields
}

func InferContractField(
	rows []map[string]any,
	fieldName string,
) (model.ContractFieldType, bool) {
	if len(rows) == 0 {
		return model.ContractFieldTypeUnknown, false
	}

	inferred := model.ContractFieldTypeUnknown
	nullable := false
	for _, row := range rows {
		value, exists := row[fieldName]
		if !exists || value == nil {
			nullable = true
			continue
		}

		current := InferFieldType(value)
		inferred = MergeContractFieldType(inferred, current)
	}

	return inferred, nullable
}

func MergeContractFieldType(
	current model.ContractFieldType,
	next model.ContractFieldType,
) model.ContractFieldType {
	if next == model.ContractFieldTypeUnknown {
		return current
	}
	if current == model.ContractFieldTypeUnknown {
		return next
	}
	if current == next {
		return current
	}
	if (current == model.ContractFieldTypeValueInteger && next == model.ContractFieldTypeValueNumber) ||
		(current == model.ContractFieldTypeValueNumber && next == model.ContractFieldTypeValueInteger) {
		return model.ContractFieldTypeValueNumber
	}

	return model.ContractFieldTypeUnknown
}

func MissingRequiredField(
	columnNames []string,
	rows []map[string]any,
	contract *Contract,
) (string, bool) {
	if contract == nil {
		return "", false
	}

	columnSet := make(map[string]struct{}, len(columnNames))
	for _, name := range columnNames {
		columnSet[name] = struct{}{}
	}

	for _, field := range contract.Fields {
		if !field.Required {
			continue
		}

		if _, exists := columnSet[field.Name]; exists {
			continue
		}

		if len(rows) > 0 && FieldPathExists(rows, field.Name) {
			continue
		}

		return field.Name, true
	}

	return "", false
}
