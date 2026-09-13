package formpayload_test

import (
	"bytes"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/require"

	formpayload "github.com/Braidplane/cli/pkg/payload/form"
)

func TestParseURLEncodedFormBodyPreservesRepeatedFields(t *testing.T) {
	t.Parallel()

	form, err := formpayload.ParseURLEncodedFormBody([]byte("name=Alice&tag=urgent&tag=customer"))
	require.NoError(t, err)

	require.Equal(t, formpayload.FormEncodingURLEncoded, form.Encoding)
	require.Equal(t, []formpayload.FormField{
		{Name: "name", ValueCount: 1, SizeBytes: 5, Values: []string{"Alice"}},
		{Name: "tag", ValueCount: 2, SizeBytes: 14, Values: []string{"urgent", "customer"}},
	}, form.Fields)
}

func TestParseMultipartFormBodyCapturesFileMetadataAndContent(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	require.NoError(t, writer.WriteField("name", "Alice"))
	part, err := writer.CreateFormFile("attachment", "import.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("id\n1\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	form, err := formpayload.ParseMultipartFormBody(
		writer.FormDataContentType(),
		buffer.Bytes(),
		int64(buffer.Len()),
	)
	require.NoError(t, err)

	require.Equal(t, formpayload.FormEncodingMultipart, form.Encoding)
	require.Equal(t, []formpayload.FormField{{
		Name:       "name",
		ValueCount: 1,
		SizeBytes:  5,
		Values:     []string{"Alice"},
	}}, form.Fields)
	require.Len(t, form.Files, 1)
	require.Equal(t, "attachment", form.Files[0].Name)
	require.Equal(t, "import.csv", form.Files[0].Filename)
	require.Equal(t, []byte("id\n1\n"), form.Files[0].Content)
	require.NotEmpty(t, form.Files[0].SHA256)
}
