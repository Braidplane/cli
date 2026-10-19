package decimal_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/stretchr/testify/require"
)

func TestExactArithmeticAndRounding(t *testing.T) {
	for _, test := range []struct {
		left, right, op, want string
		inexact               bool
	}{
		{"0.1", "0.2", "+", "0.3", false},
		{"-2.675", "1", "*", "-2.675", false},
		{"9007199254740993", "0.1", "+", "9007199254740993.1", false},
		{"1", "3", "/", "0.33333333333333333333333333333333333333", true},
	} {
		t.Run(test.left+test.op+test.right, func(t *testing.T) {
			left, err := decimal.Parse(test.left)
			require.NoError(t, err)
			right, err := decimal.Parse(test.right)
			require.NoError(t, err)
			result, err := left.Operation(right, test.op)
			require.NoError(t, err)
			require.Equal(t, test.want, result.String())
			require.Equal(t, test.inexact, result.Inexact())
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.Equal(t, test.want, string(encoded))
		})
	}
	for _, test := range []struct{ input, want string }{{"2.675", "2.68"}, {"2.665", "2.66"}, {"-2.675", "-2.68"}, {"0.1", "0.10"}} {
		value, err := decimal.Parse(test.input)
		require.NoError(t, err)
		result, err := value.Quantize(5, 2)
		require.NoError(t, err)
		require.Equal(t, test.want, result.String())
	}
}

func TestRejectInvalidAndUnboundedValues(t *testing.T) {
	for _, input := range []string{"$1.00", "1,000.00", " 1.0", "NaN", "Inf", "01.2", strings.Repeat("1", 129), strings.Repeat("1", 39), "1e1000000000", "1e-309", "99e308"} {
		_, err := decimal.Parse(input)
		require.True(t, xerror.Is(err, decimal.ErrConversion), input)
	}
	_, err := decimal.From(0.1)
	require.Error(t, err)
	value, err := decimal.Parse("999.995")
	require.NoError(t, err)
	_, err = value.Quantize(5, 2)
	require.Error(t, err)
	one, err := decimal.Parse("1")
	require.NoError(t, err)
	zero, err := decimal.Parse("0")
	require.NoError(t, err)
	_, err = one.Operation(zero, "/")
	require.Error(t, err)
	large, err := decimal.Parse(strings.Repeat("9", 38))
	require.NoError(t, err)
	_, err = large.Operation(one, "+")
	require.NoError(t, err) // Exactly representable with a bounded exponent.
	_, err = large.Operation(large, "*")
	require.Error(t, err)
	tiny, err := decimal.Parse("1e-308")
	require.NoError(t, err)
	_, err = one.Operation(tiny, "+")
	require.Error(t, err)
}

func BenchmarkDecimalBounded(b *testing.B) {
	for _, input := range []string{"0.1", "9007199254740993.1", strings.Repeat("9", 38), strings.Repeat("9", 129), "1e1000000000"} {
		b.Run(input, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = decimal.Parse(input)
			}
		})
	}
}

func BenchmarkDecimalOperations(b *testing.B) {
	for _, test := range []struct{ name, left, right, operation string }{
		{"addition", "0.1", "0.2", "+"},
		{"division", "1", "3", "/"},
		{"bounded-exponent-alignment", "1e308", "1e-308", "+"},
	} {
		b.Run(test.name, func(b *testing.B) {
			left, err := decimal.Parse(test.left)
			if err != nil {
				b.Fatal(err)
			}
			right, err := decimal.Parse(test.right)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				_, _ = left.Operation(right, test.operation)
			}
		})
	}
	b.Run("sum-100", func(b *testing.B) {
		input, err := decimal.Parse("0.10")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			sum := decimal.Value{}
			for range 100 {
				sum, err = sum.Operation(input, "+")
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func TestDecimalJSONRoundTripBoundedExponents(t *testing.T) {
	for _, input := range []string{"1e308", "1e-308", "0.00000001"} {
		value, err := decimal.Parse(input)
		require.NoError(t, err)
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		roundTrip, err := decimal.Parse(string(encoded))
		require.NoError(t, err)
		require.Zero(t, value.Cmp(roundTrip))
	}
}
