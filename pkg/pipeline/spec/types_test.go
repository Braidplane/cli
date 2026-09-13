package spec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSinkIdempotencyClassIsReplaySafe keeps replay classification aligned with ledger semantics.
func TestSinkIdempotencyClassIsReplaySafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		class SinkIdempotencyClass
		want  bool
	}{
		{name: "NaturallyIdempotent", class: SinkIdempotencyClassNaturallyIdempotent, want: true},
		{name: "IdempotentWithKey", class: SinkIdempotencyClassIdempotentWithKey, want: true},
		{name: "DeduplicatedByLedger", class: SinkIdempotencyClassDeduplicatedByLedger, want: true},
		{name: "Unsafe", class: SinkIdempotencyClassUnsafe, want: false},
		{name: "Unknown", class: SinkIdempotencyClassUnknown, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.class.IsReplaySafe())
		})
	}
}
