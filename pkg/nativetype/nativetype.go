package nativetype

import "strings"

const (
	TypeString   = "string"
	TypeInteger  = "integer"
	TypeNumber   = "number"
	TypeBoolean  = "boolean"
	TypeObject   = "object"
	TypeArray    = "array"
	TypeNull     = "null"
	TypeDatetime = "datetime"
	TypeUnknown  = "unknown"
)

// NormalizeJSONLike normalizes JSON/CSV-style native field types into the canonical vocabulary.
func NormalizeJSONLike(native string) string {
	n := strings.ToLower(strings.TrimSpace(native))
	switch n {
	case "":
		return TypeUnknown
	case TypeString:
		return TypeString
	case TypeInteger, "int", "int32", "int64", "long":
		return TypeInteger
	case TypeNumber, "float", "double", "decimal", "decimal128":
		return TypeNumber
	case TypeBoolean, "bool":
		return TypeBoolean
	case TypeObject, "document":
		return TypeObject
	case TypeArray:
		return TypeArray
	case TypeNull:
		return TypeNull
	case "date", TypeDatetime, "timestamp", "date-time":
		return TypeDatetime
	default:
		return n
	}
}

// MergeJSONLike merges two JSON-like types for a single field path.
func MergeJSONLike(existing, incoming string) string {
	e := NormalizeJSONLike(existing)
	i := NormalizeJSONLike(incoming)

	if e == i {
		return e
	}

	if e == TypeNull {
		return i
	}
	if i == TypeNull {
		return e
	}

	if (e == TypeInteger && i == TypeNumber) ||
		(e == TypeNumber && i == TypeInteger) {
		return TypeNumber
	}

	return TypeString
}
