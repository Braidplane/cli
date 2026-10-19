// Package decimal implements the bounded base-ten pipeline numeric contract.
package decimal

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/cockroachdb/apd/v3"
)

const MaxPrecision = 38

var ErrConversion = xerror.New(xerror.KindInvalidInput, "PIPELINE_DECIMAL_CONVERSION", "invalid bounded decimal conversion")

var tokenPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// Value is immutable; operations allocate a destination and never mutate operands.
// The zero value represents decimal zero.
type Value struct {
	d          apd.Decimal
	conditions apd.Condition
}

func context(precision int, rounding bool) apd.Context {
	traps := apd.DefaultTraps
	if !rounding {
		traps |= apd.Inexact
	}
	return apd.Context{Precision: uint32(precision), MinExponent: -308, MaxExponent: 308, Rounding: apd.RoundHalfEven, Traps: traps} //nolint:gosec // Callers use 38 or a declaration validated in [1,38].
}

// Parse rejects hostile tokens before coefficient allocation and never rounds input.
func Parse(text string) (Value, error) {
	if len(text) > 128 || !tokenPattern.MatchString(text) {
		return Value{}, ErrConversion.Detail("expected a finite decimal token of at most 128 bytes")
	}
	mantissa := text
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent < -308 || exponent > 308 {
			return Value{}, ErrConversion.Detail("decimal exponent must be between -308 and 308")
		}
		mantissa = text[:index]
	}
	digits := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(mantissa, "-"), ".", ""), "0")
	if len(digits) > MaxPrecision {
		return Value{}, ErrConversion.Detail("decimal coefficient exceeds 38 digits")
	}
	var result Value
	ctx := context(MaxPrecision, false)
	_, flags, err := ctx.SetString(&result.d, text)
	return finish(result, flags, err)
}

// From accepts exact decimal tokens and integers. Binary floats require Cast.
func From(value any) (Value, error) {
	switch v := value.(type) {
	case Value:
		return v, nil
	case string:
		return Parse(v)
	case json.Number:
		return Parse(v.String())
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return Parse(fmt.Sprint(v))
	default:
		return Value{}, ErrConversion.Detailf("type %T requires an explicit decimal cast", value)
	}
}

// Cast exposes the already-rounded float's shortest decimal representation.
func Cast(value any) (Value, error) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Value{}, ErrConversion.Detail("decimal cast requires a finite value")
		}
		return Parse(strconv.FormatFloat(v, 'g', -1, 64))
	case float32:
		return Parse(strconv.FormatFloat(float64(v), 'g', -1, 32))
	default:
		return From(value)
	}
}

func ValidateDeclaration(precision, scale int) error {
	if precision < 1 || precision > MaxPrecision || scale < 0 || scale > precision {
		return ErrConversion.Detail("decimal requires 1 <= precision <= 38 and 0 <= scale <= precision")
	}
	return nil
}

func (v Value) Quantize(precision, scale int) (Value, error) {
	if err := ValidateDeclaration(precision, scale); err != nil {
		return Value{}, err
	}
	result := Value{conditions: v.conditions}
	ctx := context(precision, true)
	flags, err := ctx.Quantize(&result.d, &v.d, -int32(scale)) //nolint:gosec // Declaration validation bounds scale to [0,38].
	return finish(result, flags, err)
}

func (v Value) Operation(other Value, operation string) (Value, error) {
	result := Value{conditions: v.conditions | other.conditions}
	ctx := context(MaxPrecision, operation == "/")
	var flags apd.Condition
	var err error
	switch operation {
	case "+":
		flags, err = ctx.Add(&result.d, &v.d, &other.d)
	case "-":
		flags, err = ctx.Sub(&result.d, &v.d, &other.d)
	case "*":
		flags, err = ctx.Mul(&result.d, &v.d, &other.d)
	case "/":
		flags, err = ctx.Quo(&result.d, &v.d, &other.d)
	default:
		return Value{}, ErrConversion.Detail("unsupported decimal operation")
	}
	return finish(result, flags, err)
}

func finish(result Value, flags apd.Condition, err error) (Value, error) {
	if err != nil || result.d.Form != apd.Finite {
		return Value{}, ErrConversion.Detailf("decimal operation rejected (%s)", flags)
	}
	result.conditions |= flags
	return result, nil
}

func (v Value) String() string               { return v.d.Text('f') }
func (v Value) MarshalJSON() ([]byte, error) { return []byte(v.d.String()), nil }
func (v Value) Inexact() bool                { return v.conditions.Inexact() }
func (v Value) Rounded() bool                { return v.conditions.Rounded() }
func (v Value) Cmp(other Value) int          { return v.d.Cmp(&other.d) }

// Rat is exact and bounded by the decimal exponent contract.
func (v Value) Rat() *big.Rat {
	result, _ := new(big.Rat).SetString(v.d.String())
	return result
}

// EstimatedBytes includes coefficient storage as well as the serialized token.
func (v Value) EstimatedBytes() int64 { return int64(len(v.String()) + 32) }
