package payload

import (
	"encoding/json"
	"math"
	"math/big"
	"math/bits"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Braidplane/cli/pkg/decimal"
)

// ValidateStructuralValue checks one logical value without changing it. Encoding
// normalization and batch/root handling remain the caller's separate obligation.
func ValidateStructuralValue(s *Structure, value any, limits StructuralLimits) error {
	b, err := newStructuralBudget(limits)
	if err != nil {
		return err
	}
	if err := b.schema(s); err != nil {
		return err
	}
	// Inspect the complete input first, including open properties. A mismatch
	// cannot hide an overflowing or cyclic branch behind an early return.
	if err := b.valueBudget(value, 0, nil); err != nil {
		return err
	}
	return b.value(s.Root, value, nil)
}

func (b *structuralBudget) valueBudget(v any, depth int, at StructuralLocator) error {
	if depth >= b.limits.Depth {
		return structuralError(StructureLimit, at)
	}
	if err := b.spend(1, 8, 1, at); err != nil {
		return err
	}
	switch v := v.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return nil
	case float32:
		return b.valueBudgetFloat(float64(v), at)
	case float64:
		return b.valueBudgetFloat(v, at)
	case string:
		if !utf8.ValidString(v) {
			return structuralError(StructureInvalid, at)
		}
		return b.spend(0, len(v), 1, at)
	case json.Number:
		if err := b.spend(0, len(v), 1, at); err != nil {
			return err
		}
		if _, err := decimal.Parse(string(v)); err != nil {
			return structuralError(StructureUnsupported, at)
		}
	case decimal.Value:
		return b.spend(0, int(v.EstimatedBytes()), 1, at)
	case []any:
		if len(v) > b.limits.Nodes-b.nodes {
			return structuralError(StructureLimit, at)
		}
		for _, item := range v {
			if err := b.valueBudget(item, depth+1, at); err != nil {
				return err
			}
		}
	case map[string]any:
		if len(v) > b.limits.Nodes-b.nodes {
			return structuralError(StructureLimit, at)
		}
		keys, err := b.structuralKeys(v, at)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := b.spend(0, len(key), 1, at); err != nil {
				return err
			}
			if !utf8.ValidString(key) {
				return structuralError(StructureInvalid, at)
			}
			if err := b.valueBudget(v[key], depth+1, at); err != nil {
				return err
			}
		}
	default:
		return structuralError(StructureUnsupported, at)
	}
	return nil
}

func (b *structuralBudget) structuralKeys(object map[string]any, at StructuralLocator) ([]string, error) {
	if err := b.spend(0, 16*len(object), len(object), at); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(object))
	longest := 0
	for key := range object {
		if err := b.spend(0, 0, len(key), at); err != nil {
			return nil, err
		}
		longest = max(longest, len(key))
		keys = append(keys, key)
	}
	// Precharge a deterministic n log n comparison allowance, including key
	// bytes; charging actual comparisons would depend on initial map order.
	steps := len(keys) * (bits.Len(uint(len(keys))) + 1)
	if steps > (b.limits.Work-b.work)/(longest+1) {
		return nil, structuralError(StructureLimit, at)
	}
	if err := b.spend(0, 0, steps*(longest+1), at); err != nil {
		return nil, err
	}
	slices.Sort(keys)
	return keys, nil
}

func (b *structuralBudget) valueBudgetFloat(v float64, at StructuralLocator) error {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return structuralError(StructureUnsupported, at)
	}
	return nil
}

func (b *structuralBudget) value(t *StructuralType, v any, at StructuralLocator) error {
	if err := b.spend(0, 0, 1, at); err != nil {
		return err
	}
	if v == nil {
		if t.Nullable {
			return nil
		}
		return structuralError(StructureNull, at)
	}
	switch t.Kind {
	case StructuralObject:
		object, ok := v.(map[string]any)
		if !ok {
			return structuralError(StructureType, at)
		}
		known := make(map[string]bool, len(t.Properties))
		for i, p := range t.Properties {
			loc := structuralAt(at, i)
			if err := b.spend(0, 0, 1, loc); err != nil {
				return err
			}
			known[p.Name] = true
			value, exists := object[p.Name]
			if !exists {
				if p.Required {
					return structuralError(StructurePresence, loc)
				}
				continue
			}
			if err := b.value(p.Type, value, loc); err != nil {
				return err
			}
		}
		if t.Additional == StructuralClosed {
			for name := range object {
				if !known[name] {
					return structuralError(StructureConstraint, at)
				}
			}
		}
	case StructuralMap:
		object, ok := v.(map[string]any)
		if !ok {
			return structuralError(StructureType, at)
		}
		if !structuralLength(t, len(object)) {
			return structuralError(StructureConstraint, at)
		}
		keys, err := b.structuralKeys(object, at)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := b.value(t.Keys, key, structuralAt(at, -2)); err != nil {
				return err
			}
			if err := b.value(t.Values, object[key], structuralAt(at, -3)); err != nil {
				return err
			}
		}
	case StructuralArray, StructuralVector:
		array, ok := v.([]any)
		if !ok {
			return structuralError(StructureType, at)
		}
		if !structuralLength(t, len(array)) || (t.Kind == StructuralVector && len(array) != t.Dimension) {
			return structuralError(StructureConstraint, at)
		}
		for i, item := range array {
			if err := b.value(t.Items, item, structuralAt(at, -1)); err != nil {
				return err
			}
			if t.Unique {
				for _, previous := range array[:i] {
					equal, err := b.equal(previous, item, at)
					if err != nil {
						return err
					}
					if equal {
						return structuralError(StructureConstraint, at)
					}
				}
			}
		}
	default:
		if !structuralScalarValue(t, v) {
			return structuralError(StructureType, at)
		}
	}
	if t.Enum != nil {
		found := false
		for _, allowed := range t.Enum {
			equal, err := b.equal(allowed, v, at)
			if err != nil {
				return err
			}
			found = found || equal
		}
		if !found {
			return structuralError(StructureConstraint, at)
		}
	}
	return nil
}

func structuralLength(t *StructuralType, length int) bool {
	return (t.MinLength == nil || length >= *t.MinLength) && (t.MaxLength == nil || length <= *t.MaxLength)
}

func structuralScalarValue(t *StructuralType, v any) bool {
	if structuralNumeric(t.Kind) {
		number, ok := structuralNumber(v)
		if !ok {
			return false
		}
		switch t.Kind {
		case StructuralInteger:
			// Floats and decimals are not repaired into integers.
			switch v.(type) {
			case float32, float64, decimal.Value:
				return false
			}
			minimum, maximum := structuralIntegerRange(t.Bits)
			if !number.IsInt() || number.Cmp(minimum) < 0 || number.Cmp(maximum) > 0 {
				return false
			}
		case StructuralDecimal:
			switch v.(type) {
			case float32, float64:
				return false
			}
			exact, err := decimal.From(v)
			if err != nil {
				return false
			}
			quantized, err := exact.Quantize(t.Precision, t.Scale)
			if err != nil || exact.Cmp(quantized) != 0 {
				return false
			}
		case StructuralNumber:
			if _, decimalValue := v.(decimal.Value); decimalValue {
				return false
			}
			f, _ := number.Float64()
			if t.Bits == 32 {
				f = float64(float32(f))
			}
			if math.IsInf(f, 0) || math.IsNaN(f) || new(big.Rat).SetFloat64(f).Cmp(number) != 0 {
				return false
			}
		default:
			return false
		}
		return (t.Min == nil || number.Cmp(t.Min.Rat()) >= 0) && (t.Max == nil || number.Cmp(t.Max.Rat()) <= 0)
	}
	if t.Kind == StructuralBoolean {
		_, ok := v.(bool)
		return ok
	}
	text, ok := v.(string)
	if !ok || !utf8.ValidString(text) {
		return false
	}
	if t.Kind == StructuralString {
		return structuralLength(t, utf8.RuneCountInString(text)) && (t.Format == "" || matchesFormat(text, t.Format))
	}
	layout := map[StructuralKind]string{
		StructuralDate: "2006-01-02", StructuralLocalTime: "15:04:05.999999999",
		StructuralLocalDatetime: "2006-01-02T15:04:05.999999999", StructuralInstant: time.RFC3339Nano,
	}[t.Kind]
	if layout == "" || strings.Contains(text, ",") {
		return false
	}
	if t.Format != "" {
		fraction := strings.IndexByte(text, '.')
		if t.Format == "YYYY-MM-DDTHH:mm:ss.SSSZ" {
			if fraction != 19 || len(text) < 24 || text[23] != 'Z' && text[23] != '+' && text[23] != '-' {
				return false
			}
		} else if fraction >= 0 {
			return false
		}
	}
	// time.Parse accepts more than nine fractional digits by truncating them.
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		digits := 0
		for _, char := range text[dot+1:] {
			if char < '0' || char > '9' {
				break
			}
			digits++
		}
		if digits == 0 || digits > 9 {
			return false
		}
	}
	clock := text
	if t.Kind == StructuralLocalDatetime || t.Kind == StructuralInstant {
		if len(text) < 19 || text[10] != 'T' {
			return false
		}
		clock = text[11:]
	}
	if t.Kind != StructuralDate && (len(clock) < 8 || clock[2] != ':' || clock[5] != ':') {
		return false
	}
	if t.Kind == StructuralInstant && len(text) >= 6 && text[len(text)-1] != 'Z' {
		offset := text[len(text)-6:]
		if offset[1:3] > "23" || offset[4:6] > "59" {
			return false
		}
	}
	_, err := time.Parse(layout, text)
	return err == nil
}

func structuralNumber(v any) (*big.Rat, bool) {
	switch v := v.(type) {
	case float32:
		return structuralNumber(float64(v))
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(v), true
	case string:
		return nil, false
	default:
		exact, err := decimal.From(v)
		if err != nil {
			return nil, false
		}
		return exact.Rat(), true
	}
}

func structuralIntegerRange(bits int) (*big.Rat, *big.Rat) {
	upper := new(big.Int).Lsh(big.NewInt(1), uint(bits-1)) //nolint:gosec // Validated signed width.
	return new(big.Rat).SetInt(new(big.Int).Neg(upper)), new(big.Rat).SetInt(new(big.Int).Sub(upper, big.NewInt(1)))
}

// equal uses semantic numeric equality and charges even failed comparisons.
func (b *structuralBudget) equal(left, right any, at StructuralLocator) (bool, error) {
	if err := b.spend(0, 0, 1, at); err != nil {
		return false, err
	}
	if number, ok := structuralNumber(left); ok {
		other, ok := structuralNumber(right)
		return ok && number.Cmp(other) == 0, nil
	}
	switch left := left.(type) {
	case nil:
		return right == nil, nil
	case string:
		other, ok := right.(string)
		if err := b.spend(0, 0, len(left), at); err != nil {
			return false, err
		}
		return ok && left == other, nil
	case bool:
		other, ok := right.(bool)
		return ok && left == other, nil
	case []any:
		other, ok := right.([]any)
		if !ok || len(left) != len(other) {
			return false, nil
		}
		for i := range left {
			equal, err := b.equal(left[i], other[i], at)
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	case map[string]any:
		other, ok := right.(map[string]any)
		if !ok || len(left) != len(other) {
			return false, nil
		}
		keys, err := b.structuralKeys(left, at)
		if err != nil {
			return false, err
		}
		for _, key := range keys {
			item, exists := other[key]
			if !exists {
				return false, nil
			}
			equal, err := b.equal(left[key], item, at)
			if err != nil || !equal {
				return equal, err
			}
		}
		return true, nil
	default:
		return false, nil
	}
}
