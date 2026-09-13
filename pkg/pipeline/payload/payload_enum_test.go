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
