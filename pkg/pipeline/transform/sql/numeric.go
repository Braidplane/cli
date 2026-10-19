package sql

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/pingcap/tidb/pkg/parser/opcode"
)

// ErrNumericConversion rejects unsupported ranges and lossy integer conversions.
var ErrNumericConversion = xerror.New(xerror.KindInvalidInput, "PIPELINE_TRANSFORM_NUMERIC_CONVERSION", "unsupported transform numeric conversion")

// Numeric work is bounded to 64-bit integers and finite float64 fractions.
// Rat compares integers with binary floats without first rounding the integers.
func numericValue(value any) (*big.Rat, error) {
	switch v := value.(type) {
	case decimal.Value:
		return v.Rat(), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		integer, _ := new(big.Int).SetString(fmt.Sprint(v), 10)
		return new(big.Rat).SetInt(integer), nil
	case float32:
		return finiteNumericFloat(float64(v))
	case float64:
		return finiteNumericFloat(v)
	case json.Number:
		return numericText(v.String())
	case string:
		return numericText(strings.TrimSpace(v))
	default:
		return nil, ErrNumericConversion.Detailf("unsupported numeric type %T", value)
	}
}

func finiteNumericFloat(value float64) (*big.Rat, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, ErrNumericConversion.Detail("numeric value must be finite")
	}
	return new(big.Rat).SetFloat64(value), nil
}

func numericText(value string) (*big.Rat, error) {
	// Bound decimal/exponent parsing before Rat allocates; projection never parses
	// opaque numbers and can preserve larger literals unchanged.
	if len(value) > 128 {
		return nil, ErrNumericConversion.Detail("numeric conversion exceeds supported range")
	}
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(value[index+1:])
		if err != nil || exponent < -308 || exponent > 308 {
			return nil, ErrNumericConversion.Detail("numeric exponent exceeds supported range")
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, ErrNumericConversion.Detail("invalid or out-of-range numeric conversion")
	}
	exact, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, ErrNumericConversion.Detail("invalid numeric conversion")
	}
	if exact.IsInt() {
		if !exact.Num().IsInt64() && !exact.Num().IsUint64() {
			return nil, ErrNumericConversion.Detail("integer exceeds supported 64-bit range")
		}
		return exact, nil
	}
	return finiteNumericFloat(number)
}

func numericFloat(value *big.Rat) (float64, error) {
	number, exact := value.Float64()
	if math.IsInf(number, 0) || (value.IsInt() && !exact) {
		return 0, ErrNumericConversion.Detail("numeric conversion would lose integer precision")
	}
	return number, nil
}

func integerNumericResult(value *big.Rat) (any, error) {
	if value.Num().IsInt64() {
		return value.Num().Int64(), nil
	}
	if value.Num().IsUint64() {
		return value.Num().Uint64(), nil
	}
	return nil, ErrNumericConversion.Detail("integer result exceeds supported 64-bit range")
}

func numericOperation(left, right any, operation opcode.Op) (any, error) {
	switch operation {
	case opcode.Plus, opcode.Minus, opcode.Mul, opcode.Div:
	default:
		return nil, ErrRuntimeProjectionExpressionUnsupported
	}
	if left == nil || right == nil {
		return nil, nil //nolint:nilnil // SQL NULL is a successful result.
	}
	_, leftDecimal := left.(decimal.Value)
	_, rightDecimal := right.(decimal.Value)
	if leftDecimal || rightDecimal {
		l, err := decimalOperand(left)
		if err != nil {
			return nil, err
		}
		r, err := decimalOperand(right)
		if err != nil {
			return nil, err
		}
		var operator string
		switch operation {
		case opcode.Plus:
			operator = "+"
		case opcode.Minus:
			operator = "-"
		case opcode.Mul:
			operator = "*"
		case opcode.Div:
			operator = "/"
		default:
			return nil, ErrRuntimeProjectionExpressionUnsupported
		}
		result, err := l.Operation(r, operator)
		if err != nil {
			return nil, xerror.Wrapf("decimal arithmetic failed: %w", err)
		}
		return result, nil
	}
	l, err := numericValue(left)
	if err != nil {
		return nil, xerror.Wrapf("left operand numeric conversion failed: %w", err)
	}
	r, err := numericValue(right)
	if err != nil {
		return nil, xerror.Wrapf("right operand numeric conversion failed: %w", err)
	}
	if operation == opcode.Div && r.Sign() == 0 {
		return nil, ErrNumericConversion.Detail("division by zero")
	}
	if l.IsInt() && r.IsInt() && !numericUsesFloat(left) && !numericUsesFloat(right) {
		result := new(big.Rat)
		switch operation {
		case opcode.Plus:
			result.Add(l, r)
		case opcode.Minus:
			result.Sub(l, r)
		case opcode.Mul:
			result.Mul(l, r)
		case opcode.Div:
			result.Quo(l, r)
		default:
			return nil, ErrRuntimeProjectionExpressionUnsupported
		}
		if result.IsInt() {
			return integerNumericResult(result)
		}
	}
	lf, err := numericFloat(l)
	if err != nil {
		return nil, err
	}
	rf, err := numericFloat(r)
	if err != nil {
		return nil, err
	}
	var result float64
	switch operation {
	case opcode.Plus:
		result = lf + rf
	case opcode.Minus:
		result = lf - rf
	case opcode.Mul:
		result = lf * rf
	case opcode.Div:
		result = lf / rf
	default:
		return nil, ErrRuntimeProjectionExpressionUnsupported
	}
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return nil, ErrNumericConversion.Detail("numeric result exceeds finite float64 range")
	}
	return result, nil
}

func numericCandidate(value any) bool {
	switch v := value.(type) {
	case string:
		_, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return err == nil || xerror.Is(err, strconv.ErrRange)
	case decimal.Value, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

func numericUsesFloat(value any) bool {
	switch value.(type) {
	case float32, float64:
		return true
	default:
		return false
	}
}

func decimalOperand(value any) (decimal.Value, error) {
	converted, err := decimal.From(value)
	if err != nil {
		return decimal.Value{}, xerror.Wrapf("convert decimal operand: %w", err)
	}
	if _, ok := value.(decimal.Value); !ok && !converted.Rat().IsInt() {
		return decimal.Value{}, decimal.ErrConversion.Detail("fractional operand requires an explicit decimal cast")
	}
	return converted, nil
}

func compareDecimalOperands(left, right any) (int, bool, error) {
	_, ld := left.(decimal.Value)
	_, rd := right.(decimal.Value)
	if !ld && !rd {
		return 0, false, nil
	}
	l, err := decimalOperand(left)
	if err != nil {
		return 0, true, err
	}
	r, err := decimalOperand(right)
	if err != nil {
		return 0, true, err
	}
	return l.Cmp(r), true, nil
}
