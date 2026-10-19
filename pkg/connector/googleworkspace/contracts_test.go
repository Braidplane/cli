package googleworkspace

import (
	"encoding/json"
	"testing"
)

func TestStatementContracts(t *testing.T) {
	for _, tc := range []struct {
		name, json string
		valid      bool
	}{
		{"worksheet zero", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:C10"}`, true},
		{"ordered update", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":7,"range":"AA2:AB3","columns":["a","b"],"rows":"{{input}}"}`, true},
		{"empty rows", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A2","columns":["a"],"rows":[]}`, false},
		{"wrong height", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A2","columns":["a"],"rows":[{"a":1}]}`, false},
		{"exact height", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A2","columns":["a"],"rows":[{"a":1},{"a":2}]}`, true},
		{"scalar", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":7}`, false},
		{"boolean", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":true}`, false},
		{"single object", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":{"a":1}}`, false},
		{"other string", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":"arbitrary"}`, false},
		{"null row", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":[null]}`, false},
		{"scalar row", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A1","columns":["a"],"rows":[1]}`, false},
		{"missing worksheet", `{"type":"google_workspace","operation":"read","range":"A1:A2"}`, false},
		{"negative worksheet", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":-1,"range":"A1:A2"}`, false},
		{"unbounded", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A:C"}`, false},
		{"title", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"Tab!A1:C2"}`, false},
		{"reversed", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"C2:A1"}`, false},
		{"overflow", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:A999999999999999"}`, false},
		{"wrong width", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:C2","columns":["a"],"rows":"{{input}}"}`, false},
		{"duplicate keys", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:B2","columns":["a","a"],"rows":"{{input}}"}`, false},
		{"blank key", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"update","worksheet_id":0,"range":"A1:A2","columns":[" "],"rows":"{{input}}"}`, false},
		{"read with null rows", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:A2","rows":null}`, false},
		{"read with null mapping", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:A2","columns":null}`, false},
		{"unknown field", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:A2","append":true}`, false},
		{"append", `{"type":"google_workspace","operation":"append","worksheet_id":0,"range":"A1:A2"}`, false},
		{"trailing document", `{"type":"google_workspace","resource":"spreadsheet","spreadsheet_id":"book","operation":"read","worksheet_id":0,"range":"A1:A2"} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeStatement([]byte(tc.json))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
	r, err := ParseRange("Z9:AB12")
	if err != nil || r != (Rectangle{9, 12, 26, 28}) {
		t.Fatalf("rectangle=%+v error=%v", r, err)
	}
}

func TestConfigConsentAndIdentity(t *testing.T) {
	if (Config{}).ConsentMode() != SelectedFiles {
		t.Fatal("selected-file default missing")
	}
	for _, mode := range []string{"", SelectedFiles} {
		if err := (Config{AccessMode: mode}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if (Config{AccessMode: "invalid"}).Validate() == nil {
		t.Fatal("accepted invalid mode")
	}
	for _, raw := range []string{`null`, `[]`, `{"spreadsheet_id":"book"}`, `{"access_token":"secret"}`, `{"access_mode":"bad"}`} {
		var cfg Config
		if json.Unmarshal([]byte(raw), &cfg) == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
}

func TestSelectedFileGrantsRejectBroadAccess(t *testing.T) {
	for _, scopes := range [][]string{nil, {"openid", "email"}, {"https://www.googleapis.com/auth/drive"}, {FileScope, "https://www.googleapis.com/auth/drive.readonly"}, {FileScope, "https://www.googleapis.com/auth/spreadsheets"}} {
		if ValidFileScopes(scopes) {
			t.Fatalf("accepted unsupported scopes %v", scopes)
		}
	}
	if !ValidFileScopes([]string{"openid", "email", FileScope}) || !ValidFileScopes([]string{FileScope}) {
		t.Fatal("rejected selected-file grant")
	}
	if (Config{AccessMode: "all_files"}).Validate() == nil {
		t.Fatal("accepted retired mode")
	}
}
