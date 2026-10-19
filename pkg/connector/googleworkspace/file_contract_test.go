package googleworkspace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileStatementContractBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"read JSON", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"json"}`, true},
		{"read CSV", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"csv","delimiter":","}`, true},
		{"read opaque file", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"file"}`, true},
		{"list filters", `{"type":"google_workspace","resource":"file","operation":"list","folder_id":"folder","drive_id":"drive","mime_type":"application/json","page_token":"next"}`, true},
		{"create complete", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"crm.json","format":"json","content":"{{input}}"}`, true},
		{"update complete", `{"type":"google_workspace","resource":"file","operation":"update","file_id":"report","format":"json","content":"{{input}}"}`, true},
		{"missing resource", `{"type":"google_workspace","operation":"read","file_id":"report","format":"json"}`, false},
		{"create requires folder", `{"type":"google_workspace","resource":"file","operation":"create","name":"crm.json","format":"json","content":"{{input}}"}`, false},
		{"create requires name", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","format":"json","content":"{{input}}"}`, false},
		{"create requires content", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"crm.json","format":"json"}`, false},
		{"create rejects null content", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"crm.json","format":"json","content": null }`, false},
		{"update rejects boolean content", `{"type":"google_workspace","resource":"file","operation":"update","file_id":"report","format":"json","content":true}`, false},
		{"update rejects numeric content", `{"type":"google_workspace","resource":"file","operation":"update","file_id":"report","format":"json","content":1}`, false},
		{"update requires file ID", `{"type":"google_workspace","resource":"file","operation":"update","format":"json","content":"{{input}}"}`, false},
		{"unsupported format", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"xml"}`, false},
		{"native conversion", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"report","format":"file","content":"{{input}}","mime_type":"application/vnd.google-apps.document"}`, false},
		{"invalid MIME parameters", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"report","format":"json","content":"{{input}}","mime_type":"application/json; charset=utf-8"}`, false},
		{"list cannot claim format", `{"type":"google_workspace","resource":"file","operation":"list","format":null}`, false},
		{"read cannot carry write content", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"json","content":null}`, false},
		{"create cannot carry update ID", `{"type":"google_workspace","resource":"file","operation":"create","folder_id":"folder","name":"report","format":"json","content":"{{input}}","file_id":null}`, false},
		{"file cannot carry sheet fields", `{"type":"google_workspace","resource":"file","operation":"read","file_id":"report","format":"json","worksheet_id":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statement, err := DecodeStatement([]byte(tc.raw))
			if tc.valid {
				require.NoError(t, err)
				if statement.Operation == "list" {
					require.True(t, statement.IsRead(), "listing is read-only")
				}
				return
			}
			require.Error(t, err)
		})
	}
}
