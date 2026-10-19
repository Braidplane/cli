package googleworkspace

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Braidplane/cli/pkg/xerror"
	"github.com/stretchr/testify/require"
)

func TestNativeStatementContracts(t *testing.T) {
	for _, resource := range []string{"document", "presentation"} {
		for _, tc := range []struct {
			operation, fields string
			valid             bool
		}{
			{"read", `,"file_id":"native"`, true},
			{"export", `,"file_id":"native"`, true},
			{"create", `,"folder_id":"folder","name":"Blank"`, true},
			{"update", `,"file_id":"native","match_text":"{{name}}","content":""`, true},
			{"update", `,"file_id":"native","match_text":"{{name}}","content":"{{input}}"`, true},
			{"update", `,"file_id":"native","match_text":"x","content":null`, false},
			{"update", `,"file_id":"native","match_text":"x","content":[]`, false},
			{"update", `,"file_id":"native","content":"replacement"`, false},
			{"read", `,"file_id":"native","format":"json"`, false},
			{"export", `,"file_id":"native","mime_type":"text/plain"`, false},
			{"create", `,"folder_id":"folder","name":"Blank","content":"x"`, false},
			{"create", `,"folder_id":"folder","name":"../Blank"`, false},
			{"list", ``, false},
		} {
			t.Run(resource+tc.operation+tc.fields, func(t *testing.T) {
				s, err := DecodeStatement([]byte(`{"type":"google_workspace","resource":"` + resource + `","operation":"` + tc.operation + `"` + tc.fields + `}`))
				if tc.valid {
					require.NoError(t, err)
					require.Equal(t, tc.operation == "read" || tc.operation == "export", s.IsRead())
				} else {
					require.ErrorIs(t, err, xerror.ValidationFailed())
				}
			})
		}
	}
}

func TestNativeUnicodeCharacterLimits(t *testing.T) {
	for _, count := range []int{10000, 10001} {
		raw, err := json.Marshal(Statement{Type: "google_workspace", Resource: "document", Operation: "update", FileID: "native", MatchText: strings.Repeat("ą", count), Content: json.RawMessage(`""`)})
		require.NoError(t, err)
		_, err = DecodeStatement(raw)
		if count == 10000 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, xerror.ValidationFailed())
		}
	}
	for _, count := range []int{256, 257} {
		raw, err := json.Marshal(Statement{Type: "google_workspace", Resource: "presentation", Operation: "create", FolderID: "folder", Name: strings.Repeat("ą", count)})
		require.NoError(t, err)
		_, err = DecodeStatement(raw)
		if count == 256 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, xerror.ValidationFailed())
		}
	}
}
