package filepayload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewFilePayloadMetadataComputesSafeMetadata(t *testing.T) {
	t.Parallel()

	metadata := NewFilePayloadMetadata(" report.pdf ", "Application/PDF; charset=binary", []byte("hello"))

	require.Equal(t, "report.pdf", metadata.Filename)
	require.Equal(t, "application/pdf", metadata.MIMEType)
	require.EqualValues(t, 5, metadata.SizeBytes)
	require.Equal(t, FilePayloadDigestSHA256, metadata.DigestAlgorithm)
	require.Equal(
		t,
		"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		metadata.Digest,
	)
}

func TestNormalizeFilePayloadMIMETypeDefaultsUnknownValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, FilePayloadDefaultMIMEType, NormalizeFilePayloadMIMEType(""))
	require.Equal(t, FilePayloadDefaultMIMEType, NormalizeFilePayloadMIMEType("not a media type"))
}

func TestNormalizeFilePayloadFilenameKeepsOnlyBasename(t *testing.T) {
	t.Parallel()

	require.Equal(t, "users.pdf", NormalizeFilePayloadFilename("../../users.pdf"))
	require.Equal(t, "users.pdf", NormalizeFilePayloadFilename(`C:\exports\users.pdf`))
	require.Empty(t, NormalizeFilePayloadFilename(".."))
}
