package payload_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Braidplane/cli/pkg/pipeline/payload"
)

func TestVerificationStatusSerializesAsString(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(payload.VerificationStatusTransitivelyVerified)
	require.NoError(t, err)

	require.JSONEq(t, `"transitively_verified"`, string(data))
}

func TestProvenanceSerializesAsString(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(payload.ProvenanceExecutionResult)
	require.NoError(t, err)

	require.JSONEq(t, `"execution_result"`, string(data))
}

func TestStructuralEnumsPreserveNamesAndRejectUnknownValues(t *testing.T) {
	for _, tc := range []struct {
		value, target any
		name          string
	}{
		{payload.StructuralLocalDatetime, new(payload.StructuralKind), "local_datetime"},
		{payload.StructuralClosed, new(payload.StructuralOpenness), "closed"},
		{payload.StructuralItemSegment, new(payload.StructuralSegmentKind), "item"},
		{payload.StructureConstraint, new(payload.StructuralCode), "structure_constraint"},
	} {
		data, err := json.Marshal(tc.value)
		require.NoError(t, err)
		require.Equal(t, `"`+tc.name+`"`, string(data))
		require.NoError(t, json.Unmarshal(data, tc.target))
		require.Error(t, json.Unmarshal([]byte(`"vendor-extension"`), tc.target))
	}
	data, err := json.Marshal(map[payload.StructuralKind]bool{payload.StructuralInteger: true})
	require.NoError(t, err)
	require.JSONEq(t, `{"integer":true}`, string(data))
}
