package redaction

import (
	"bytes"
	"encoding/json"
)

// StringSlice redacts every string element in place.
func StringSlice(values []string, redact func(string) string) {
	if redact == nil {
		return
	}
	for i, value := range values {
		values[i] = redact(value)
	}
}

// StringMap redacts string map keys and values in place.
func StringMap(values map[string]string, redact func(string) string) {
	if redact == nil {
		return
	}
	updates := make(map[string]string, len(values))
	deletes := make([]string, 0)
	for key, value := range values {
		redactedKey := redact(key)
		if redactedKey != key {
			deletes = append(deletes, key)
		}
		updates[redactedKey] = redact(value)
	}
	for _, key := range deletes {
		delete(values, key)
	}
	for key, value := range updates {
		values[key] = value
	}
}

// AnySlice redacts JSON-shaped slice elements in place.
func AnySlice(values []any, redact func(string) string) {
	if redact == nil {
		return
	}
	for i, value := range values {
		values[i] = Any(value, redact)
	}
}

// AnyMap redacts JSON-shaped map keys and values in place.
func AnyMap(values map[string]any, redact func(string) string) {
	if redact == nil {
		return
	}
	updates := make(map[string]any, len(values))
	deletes := make([]string, 0)
	for key, value := range values {
		redactedKey := redact(key)
		if redactedKey != key {
			deletes = append(deletes, key)
		}
		updates[redactedKey] = Any(value, redact)
	}
	for _, key := range deletes {
		delete(values, key)
	}
	for key, value := range updates {
		values[key] = value
	}
}

// Any redacts supported JSON-shaped values and returns redacted copies for maps and slices.
func Any(value any, redact func(string) string) any {
	if redact == nil {
		return value
	}
	switch typed := value.(type) {
	case string:
		return redact(typed)
	case []byte:
		return []byte(redact(string(typed)))
	case json.RawMessage:
		return RawJSON(typed, redact)
	case map[string]string:
		return StringMapCopy(typed, redact)
	case map[string]any:
		return AnyMapCopy(typed, redact)
	case []string:
		return StringSliceCopy(typed, redact)
	case []map[string]string:
		redacted := make([]map[string]string, len(typed))
		for i, item := range typed {
			redacted[i] = StringMapCopy(item, redact)
		}
		return redacted
	case []map[string]any:
		redacted := make([]map[string]any, len(typed))
		for i, item := range typed {
			redacted[i] = AnyMapCopy(item, redact)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, item := range typed {
			redacted[i] = Any(item, redact)
		}
		return redacted
	default:
		return value
	}
}

// StringSliceCopy returns a redacted string slice without mutating the input slice.
func StringSliceCopy(values []string, redact func(string) string) []string {
	if redact == nil {
		return values
	}
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = redact(value)
	}
	return redacted
}

// StringMapCopy returns a redacted string map without mutating the input map.
func StringMapCopy(values map[string]string, redact func(string) string) map[string]string {
	if redact == nil {
		return values
	}
	redacted := make(map[string]string, len(values))
	for key, value := range values {
		redacted[redact(key)] = redact(value)
	}
	return redacted
}

// AnyMapCopy returns a redacted JSON-shaped map without mutating the input map.
func AnyMapCopy(values map[string]any, redact func(string) string) map[string]any {
	if redact == nil {
		return values
	}
	redacted := make(map[string]any, len(values))
	for key, value := range values {
		redacted[redact(key)] = Any(value, redact)
	}
	return redacted
}

// RawJSON decodes, redacts, and re-encodes JSON while falling back to whole-string redaction.
func RawJSON(raw json.RawMessage, redact func(string) string) json.RawMessage {
	if len(raw) == 0 || redact == nil {
		return raw
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return json.RawMessage(redact(string(raw)))
	}

	redacted := Any(payload, redact)
	out, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(redact(string(raw)))
	}
	return out
}
