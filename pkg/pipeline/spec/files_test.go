package spec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSupportedFile(t *testing.T) {
	t.Parallel()

	require.True(t, IsSupportedFile("pipeline.yaml"))
	require.True(t, IsSupportedFile("pipeline.yml"))
	require.True(t, IsSupportedFile("pipeline.json"))
	require.False(t, IsSupportedFile("pipeline.cue"))
	require.False(t, IsSupportedFile("pipeline.toml"))
}

func TestIsCanonicalFile(t *testing.T) {
	t.Parallel()

	require.True(t, IsCanonicalFile("single-query.pipeline.yaml"))
	require.True(t, IsCanonicalFile("single-query.pipeline.yml"))
	require.True(t, IsCanonicalFile("single-query.pipeline.json"))
	require.False(t, IsCanonicalFile("single-query.pipeline.cue"))
	require.False(t, IsCanonicalFile("pipeline.single-query.yaml"))
	require.False(t, IsCanonicalFile("notes.yaml"))
	require.False(t, IsCanonicalFile("schema.cue"))
}
