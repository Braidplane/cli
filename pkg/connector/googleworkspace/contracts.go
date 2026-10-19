package googleworkspace

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"mime"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Braidplane/cli/pkg/xerror"
)

const SelectedFiles = "selected_files"

const FileScope = "https://www.googleapis.com/auth/drive.file"

// ValidFileScopes rejects legacy broad grants rather than treating them as selected-file access.
func ValidFileScopes(scopes []string) bool {
	if !slices.Contains(scopes, FileScope) {
		return false
	}
	for _, scope := range scopes {
		switch scope {
		case FileScope, "openid", "email", "https://www.googleapis.com/auth/userinfo.email", "profile", "https://www.googleapis.com/auth/userinfo.profile":
		default:
			return false
		}
	}
	return true
}

type Config struct {
	AccessMode string `json:"access_mode,omitempty" validate:"omitempty,oneof=selected_files"`
}
type PendingInstallConfig = Config

func (c *Config) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return xerror.ValidationFailed().Detail("Workspace connection config must be an object")
	}
	type plain Config
	var value plain
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return xerror.ValidationFailed().Detail("Workspace connection config accepts only access_mode")
	}
	*c = Config(value)
	return c.Validate()
}

func (c Config) ConsentMode() string {
	if c.AccessMode == "" {
		return SelectedFiles
	}
	return c.AccessMode
}

func (c Config) Validate() error {
	if c.ConsentMode() != SelectedFiles {
		return xerror.ValidationFailed().Detail("Workspace access_mode must be selected_files")
	}
	return nil
}

var resourceID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidateID(id string) error {
	if len(id) == 0 || len(id) > 256 || !resourceID.MatchString(id) {
		return xerror.ValidationFailed().Detail("Choose a valid Google resource ID")
	}
	return nil
}

type ListOptions struct {
	FolderID  string `json:"folder_id,omitempty"`
	DriveID   string `json:"drive_id,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

func (o ListOptions) Validate() error {
	for _, id := range []string{o.FolderID, o.DriveID} {
		if id != "" {
			if err := ValidateID(id); err != nil {
				return err
			}
		}
	}
	if len(o.PageToken) > 2048 || len(o.MIMEType) > 256 {
		return xerror.ValidationFailed().Detail("Workspace discovery filters exceed their limit")
	}
	if o.MIMEType != "" {
		if t, params, err := mime.ParseMediaType(o.MIMEType); err != nil || len(params) != 0 || t != o.MIMEType {
			return xerror.ValidationFailed().Detail("Choose a valid MIME type without parameters")
		}
	}
	return nil
}

type Statement struct {
	Type          string          `json:"type"`
	Resource      string          `json:"resource"`
	Operation     string          `json:"operation"`
	SpreadsheetID string          `json:"spreadsheet_id,omitempty"`
	WorksheetID   *int            `json:"worksheet_id,omitempty"`
	Range         string          `json:"range,omitempty"`
	Columns       []string        `json:"columns,omitempty"`
	Rows          json.RawMessage `json:"rows,omitempty"`
	FileID        string          `json:"file_id,omitempty"`
	FolderID      string          `json:"folder_id,omitempty"`
	DriveID       string          `json:"drive_id,omitempty"`
	MIMEType      string          `json:"mime_type,omitempty"`
	PageToken     string          `json:"page_token,omitempty"`
	Name          string          `json:"name,omitempty"`
	Format        string          `json:"format,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	MatchText     string          `json:"match_text,omitempty"`
	Delimiter     string          `json:"delimiter,omitempty"`
	HasHeader     *bool           `json:"has_header,omitempty"`
}

func (s Statement) IsRead() bool {
	return s.Operation == "read" || s.Operation == "export" || s.Resource == "file" && s.Operation == "list"
}

func (s Statement) ListOptions() ListOptions {
	return ListOptions{s.FolderID, s.DriveID, s.MIMEType, s.PageToken}
}

// Rectangle uses one-based inclusive coordinates, matching bounded A1 ranges.
type Rectangle struct{ StartRow, EndRow, StartColumn, EndColumn int }

var rectangle = regexp.MustCompile(`^([A-Z]+)([1-9][0-9]*):([A-Z]+)([1-9][0-9]*)$`)

func ParseRange(value string) (Rectangle, error) {
	matches := rectangle.FindStringSubmatch(value)
	if len(value) > 64 || matches == nil {
		return Rectangle{}, xerror.ValidationFailed().Detail("Use a bounded A1 rectangle such as A1:C10 without a worksheet title")
	}
	startRow, err1 := strconv.ParseUint(matches[2], 10, 31)
	endRow, err2 := strconv.ParseUint(matches[4], 10, 31)
	column := func(s string) int {
		n := 0
		for _, c := range s {
			if n > (math.MaxInt32-26)/26 {
				return 0
			}
			n = n*26 + int(c-'A') + 1
		}
		return n
	}
	r := Rectangle{int(startRow), int(endRow), column(matches[1]), column(matches[3])}
	if err1 != nil || err2 != nil || r.StartColumn == 0 || r.EndColumn == 0 || r.StartRow > r.EndRow || r.StartColumn > r.EndColumn {
		return Rectangle{}, xerror.ValidationFailed().Detail("The A1 rectangle must have ordered, bounded coordinates")
	}
	return r, nil
}

func DecodeStatement(raw []byte) (Statement, error) {
	var s Statement
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, xerror.ValidationFailed().With(err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return s, xerror.ValidationFailed().Detail("Expected one Google Workspace statement")
	}
	if err := s.Validate(); err != nil {
		return s, err
	}
	allowed := map[string]bool{"type": true, "resource": true, "operation": true}
	var fields []string
	switch {
	case s.IsNative():
		switch s.Operation {
		case "read", "export":
			fields = []string{"file_id"}
		case "create":
			fields = []string{"folder_id", "name"}
		case "update":
			fields = []string{"file_id", "match_text", "content"}
		}
	case s.Resource == "spreadsheet":
		fields = []string{"spreadsheet_id", "worksheet_id", "range"}
		if s.Operation == "update" {
			fields = append(fields, "columns", "rows")
		}
	default:
		switch s.Operation {
		case "list":
			fields = []string{"folder_id", "drive_id", "mime_type", "page_token"}
		case "read":
			fields = []string{"file_id", "format", "delimiter", "has_header"}
		case "create":
			fields = []string{"folder_id", "name", "format", "content", "mime_type", "delimiter", "has_header"}
		case "update":
			fields = []string{"file_id", "format", "content", "mime_type", "delimiter", "has_header"}
		}
	}
	for _, key := range fields {
		allowed[key] = true
	}
	var submitted map[string]json.RawMessage
	if err := json.Unmarshal(raw, &submitted); err != nil {
		return s, xerror.ValidationFailed().With(err)
	}
	for key := range submitted {
		if !allowed[key] {
			return s, xerror.ValidationFailed().Detail("Workspace statement contains fields for another operation")
		}
	}
	return s, nil
}

func (s Statement) Validate() error {
	if s.Type != "google_workspace" {
		return xerror.ValidationFailed().Detail("Choose a Google Workspace statement")
	}
	if s.IsNative() {
		return s.validateNative()
	}
	if s.Resource == "file" {
		return s.validateFile()
	}
	if s.Resource != "spreadsheet" {
		return xerror.ValidationFailed().Detail("Recreate/update the Workspace operation with resource and target ID")
	}
	if err := ValidateID(s.SpreadsheetID); err != nil {
		return err
	}
	if s.WorksheetID == nil || *s.WorksheetID < 0 {
		return xerror.ValidationFailed().Detail("Choose a Google Workspace statement and a nonnegative worksheet ID")
	}
	r, err := ParseRange(s.Range)
	if err != nil {
		return err
	}
	switch s.Operation {
	case "read":
		if s.Columns != nil || s.Rows != nil {
			return xerror.ValidationFailed().Detail("Read statements cannot include update columns or rows")
		}
	case "update":
		if len(s.Columns) != r.EndColumn-r.StartColumn+1 || len(s.Rows) == 0 || bytes.Equal(bytes.TrimSpace(s.Rows), []byte("null")) {
			return xerror.ValidationFailed().Detail("Updates require rows and an ordered column mapping matching the rectangle width")
		}
		if raw := bytes.TrimSpace(s.Rows); len(raw) > 0 && raw[0] == '[' {
			var rows []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &rows); err != nil {
				return xerror.ValidationFailed().With(err)
			}
			if len(rows) != r.EndRow-r.StartRow+1 {
				return xerror.ValidationFailed().Detail("Update row count must match the rectangle height")
			}

			for _, row := range rows {
				if row == nil {
					return xerror.ValidationFailed().Detail("Update rows must be objects")
				}
			}
		} else {
			var binding string
			if err := json.Unmarshal(s.Rows, &binding); err != nil || binding != "{{input}}" {
				return xerror.ValidationFailed().Detail("Updates require an array of row objects or the {{input}} binding")
			}
		}
		seen := make(map[string]bool, len(s.Columns))
		for _, key := range s.Columns {
			if strings.TrimSpace(key) == "" || seen[key] {
				return xerror.ValidationFailed().Detail("Update column keys must be nonblank and unique")
			}
			seen[key] = true
		}
	default:
		return xerror.ValidationFailed().Detail("Choose read or update; append is unavailable")
	}
	return nil
}

func (s Statement) validateFile() error {
	if s.Operation == "list" {
		return s.ListOptions().Validate()
	}
	if s.Operation != "read" && s.Operation != "create" && s.Operation != "update" {
		return xerror.ValidationFailed().Detail("Choose list, read, create, or update for Drive files")
	}
	if s.Format != "json" && s.Format != "csv" && s.Format != "file" {
		return xerror.ValidationFailed().Detail("Choose json, csv, or file format")
	}
	if s.Format != "csv" && (s.Delimiter != "" || s.HasHeader != nil) {
		return xerror.ValidationFailed().Detail("CSV options require csv format")
	}
	if len([]rune(s.Delimiter)) > 1 {
		return xerror.ValidationFailed().Detail("CSV delimiter must be one character")
	}
	if s.Operation == "create" {
		if err := ValidateID(s.FolderID); err != nil {
			return err
		}
		if strings.TrimSpace(s.Name) == "" || len(s.Name) > 256 || strings.ContainsAny(s.Name, "/\\\x00") {
			return xerror.ValidationFailed().Detail("Choose a filename without path separators")
		}
	} else if err := ValidateID(s.FileID); err != nil {
		return err
	}
	if s.Operation != "read" {
		var content any
		if err := json.Unmarshal(s.Content, &content); err != nil {
			return xerror.ValidationFailed().Detail("File writes require content or the {{input}} binding")
		}
		switch content.(type) {
		case string, map[string]any, []any:
		default:
			return xerror.ValidationFailed().Detail("File writes require content or the {{input}} binding")
		}
	}
	if s.MIMEType != "" {
		if err := (ListOptions{MIMEType: s.MIMEType}).Validate(); err != nil {
			return err
		}
		if strings.HasPrefix(s.MIMEType, "application/vnd.google-apps.") {
			return xerror.ValidationFailed().Detail("Native Workspace document creation/conversion is unsupported")
		}
	}
	return nil
}

type File struct {
	FileID         string `json:"file_id"`
	Name           string `json:"name"`
	MIMEType       string `json:"mime_type"`
	DriveID        string `json:"drive_id,omitempty"`
	CanDownload    bool   `json:"can_download"`
	CanEdit        bool   `json:"can_edit"`
	CanAddChildren bool   `json:"can_add_children"`
}
type Discovery struct {
	Files         []File `json:"files"`
	NextPageToken string `json:"next_page_token,omitempty"`
}
type Worksheet struct {
	WorksheetID int    `json:"worksheet_id"`
	Title       string `json:"title"`
	RowCount    int    `json:"row_count"`
	ColumnCount int    `json:"column_count"`
}
type Metadata struct {
	SpreadsheetID string      `json:"spreadsheet_id"`
	Title         string      `json:"title"`
	Worksheets    []Worksheet `json:"worksheets"`
}

func (s Statement) IsNative() bool {
	return s.Resource == "document" || s.Resource == "presentation"
}

func (s Statement) NativeMIMEType() string {
	switch s.Resource {
	case "document":
		return "application/vnd.google-apps.document"
	case "presentation":
		return "application/vnd.google-apps.presentation"
	default:
		return ""
	}
}

func (s Statement) validateNative() error {
	switch s.Operation {
	case "create":
		if err := ValidateID(s.FolderID); err != nil {
			return err
		}
		if strings.TrimSpace(s.Name) == "" || utf8.RuneCountInString(s.Name) > 256 || strings.ContainsAny(s.Name, "/\\\x00") {
			return xerror.ValidationFailed().Detail("Choose a native document name without path separators")
		}
	case "read", "export", "update":
		if err := ValidateID(s.FileID); err != nil {
			return err
		}
		if s.Operation == "update" {
			var text string
			if s.MatchText == "" || utf8.RuneCountInString(s.MatchText) > 10000 || json.Unmarshal(s.Content, &text) != nil || bytes.Equal(bytes.TrimSpace(s.Content), []byte("null")) {
				return xerror.ValidationFailed().Detail("Native updates require nonempty match_text and string content")
			}
		}
	default:
		return xerror.ValidationFailed().Detail("Native documents support read, create, update, and PDF export")
	}
	return nil
}
