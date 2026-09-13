package formpayload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"sort"
	"strings"

	"github.com/Braidplane/cli/pkg/model"
	"github.com/Braidplane/cli/pkg/xerror"
)

const (
	// FormEncodingMultipart identifies multipart/form-data payloads.
	FormEncodingMultipart = "multipart/form-data"
	// FormEncodingURLEncoded identifies application/x-www-form-urlencoded payloads.
	FormEncodingURLEncoded = "application/x-www-form-urlencoded"
)

// FormData carries runtime form fields and file parts while serializing only safe metadata.
type FormData struct {
	Encoding  string      `json:"encoding,omitempty"`
	Fields    []FormField `json:"fields,omitempty"`
	Files     []FormFile  `json:"files,omitempty"`
	SizeBytes int64       `json:"size_bytes,omitempty"`
}

// FormField describes one named scalar form field.
type FormField struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type,omitempty"`
	ValueCount  int      `json:"value_count,omitempty"`
	SizeBytes   int64    `json:"size_bytes,omitempty"`
	Sensitive   bool     `json:"sensitive,omitempty"`
	Values      []string `json:"-" yaml:"-"`
}

// FormFile describes one named file form part.
type FormFile struct {
	Name        string `json:"name"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Content     []byte `json:"-" yaml:"-"`
}

// ContractField describes the contract shape derived from one or more form parts.
type ContractField struct {
	Name string
	Type model.ContractFieldType
}

type formPartSummary struct {
	name        string
	scalarCount int
	fileCount   int
}

// Clone returns a deep copy of form payload metadata and runtime-only values.
func (data *FormData) Clone() *FormData {
	if data == nil {
		return nil
	}

	fields := make([]FormField, len(data.Fields))
	for i, field := range data.Fields {
		fields[i] = field
		fields[i].Values = append([]string(nil), field.Values...)
	}

	files := make([]FormFile, len(data.Files))
	for i, file := range data.Files {
		files[i] = file
		files[i].Content = append([]byte(nil), file.Content...)
	}

	return &FormData{
		Encoding:  data.Encoding,
		Fields:    fields,
		Files:     files,
		SizeBytes: data.SizeBytes,
	}
}

// ParseURLEncodedFormBody converts a URL-encoded HTTP form body into payload metadata.
func ParseURLEncodedFormBody(body []byte) (*FormData, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, xerror.UnsupportedHTTPResponseFormat().With(err)
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fields := make([]FormField, 0, len(values))
	for _, key := range keys {
		fieldValues := values[key]
		fields = append(fields, FormField{
			Name:       key,
			ValueCount: len(fieldValues),
			SizeBytes:  formValuesSize(fieldValues),
			Values:     append([]string(nil), fieldValues...),
		})
	}

	return &FormData{
		Encoding:  FormEncodingURLEncoded,
		Fields:    fields,
		SizeBytes: int64(len(body)),
	}, nil
}

// ParseMultipartFormBody converts a multipart/form-data HTTP body into payload metadata.
func ParseMultipartFormBody(contentType string, body []byte, maxMemory int64) (*FormData, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, xerror.UnsupportedHTTPResponseFormat().With(err)
	}
	if !strings.EqualFold(mediaType, FormEncodingMultipart) || strings.TrimSpace(params["boundary"]) == "" {
		return nil, xerror.UnsupportedHTTPResponseFormat()
	}
	if maxMemory <= 0 {
		maxMemory = int64(len(body))
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	parsed, err := reader.ReadForm(maxMemory)
	if err != nil {
		return nil, xerror.UnsupportedHTTPResponseFormat().With(err)
	}
	defer func() { _ = parsed.RemoveAll() }()

	fields := multipartFields(parsed.Value)
	files, err := multipartFiles(parsed.File)
	if err != nil {
		return nil, err
	}

	return &FormData{
		Encoding:  FormEncodingMultipart,
		Fields:    fields,
		Files:     files,
		SizeBytes: int64(len(body)),
	}, nil
}

// ContractFields summarizes form part names into contract fields.
func ContractFields(form *FormData) []ContractField {
	if form == nil {
		return nil
	}

	summaries := make(map[string]*formPartSummary)
	for _, field := range form.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		summary := formPartSummaryForName(summaries, name)
		summary.scalarCount += formFieldPartCount(field)
	}
	for _, file := range form.Files {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			continue
		}
		summary := formPartSummaryForName(summaries, name)
		summary.fileCount++
	}

	names := make([]string, 0, len(summaries))
	for name := range summaries {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]ContractField, 0, len(names))
	for _, name := range names {
		fields = append(fields, ContractField{
			Name: name,
			Type: formPartContractType(summaries[name]),
		})
	}

	return fields
}

// ContractData returns a metadata-only row view suitable for contract validation.
func ContractData(form *FormData) ([]string, []map[string]any) {
	fields := ContractFields(form)
	if len(fields) == 0 {
		return nil, nil
	}

	columnNames := make([]string, 0, len(fields))
	row := make(map[string]any, len(fields))
	for _, field := range fields {
		columnNames = append(columnNames, field.Name)
		switch field.Type {
		case model.ContractFieldTypeValueString:
			row[field.Name] = ""
		case model.ContractFieldTypeValueObject:
			row[field.Name] = map[string]any{}
		case model.ContractFieldTypeValueArray:
			row[field.Name] = []any{}
		default:
			row[field.Name] = nil
		}
	}

	return columnNames, []map[string]any{row}
}

// formPartSummaryForName returns the mutable summary bucket for a form part name.
func formPartSummaryForName(
	summaries map[string]*formPartSummary,
	name string,
) *formPartSummary {
	summary := summaries[name]
	if summary == nil {
		summary = &formPartSummary{name: name}
		summaries[name] = summary
	}
	return summary
}

// formPartContractType maps scalar/file part counts to the nearest contract field type.
func formPartContractType(summary *formPartSummary) model.ContractFieldType {
	if summary == nil {
		return model.ContractFieldTypeUnknown
	}
	if summary.scalarCount > 0 && summary.fileCount > 0 {
		return model.ContractFieldTypeUnknown
	}
	if summary.fileCount > 1 || summary.scalarCount > 1 {
		return model.ContractFieldTypeValueArray
	}
	if summary.fileCount == 1 {
		return model.ContractFieldTypeValueObject
	}
	if summary.scalarCount == 1 {
		return model.ContractFieldTypeValueString
	}
	return model.ContractFieldTypeUnknown
}

// formFieldPartCount reports how many scalar values a field represents.
func formFieldPartCount(field FormField) int {
	if len(field.Values) > 0 {
		return len(field.Values)
	}
	if field.ValueCount > 0 {
		return field.ValueCount
	}
	return 1
}

// formValuesSize sums the byte length of scalar form values.
func formValuesSize(values []string) int64 {
	var size int64
	for _, value := range values {
		size += int64(len(value))
	}
	return size
}

// multipartFields converts parsed multipart scalar fields into sorted payload metadata.
func multipartFields(values map[string][]string) []FormField {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fields := make([]FormField, 0, len(keys))
	for _, key := range keys {
		fieldValues := values[key]
		fields = append(fields, FormField{
			Name:       key,
			ValueCount: len(fieldValues),
			SizeBytes:  formValuesSize(fieldValues),
			Values:     append([]string(nil), fieldValues...),
		})
	}
	return fields
}

// multipartFiles converts parsed multipart file headers into sorted payload file parts.
func multipartFiles(headers map[string][]*multipart.FileHeader) ([]FormFile, error) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	files := make([]FormFile, 0)
	for _, key := range keys {
		for _, header := range headers[key] {
			file, err := multipartFile(key, header)
			if err != nil {
				return nil, err
			}
			files = append(files, file)
		}
	}
	return files, nil
}

// multipartFile reads one multipart file part into runtime content plus safe metadata.
func multipartFile(name string, header *multipart.FileHeader) (FormFile, error) {
	if header == nil {
		return FormFile{}, nil
	}

	opened, err := header.Open()
	if err != nil {
		return FormFile{}, xerror.UnsupportedHTTPResponseFormat().With(err)
	}
	defer func() { _ = opened.Close() }()

	content, err := io.ReadAll(opened)
	if err != nil {
		return FormFile{}, xerror.UnsupportedHTTPResponseFormat().With(err)
	}

	sum := sha256.Sum256(content)
	return FormFile{
		Name:        name,
		Filename:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		SizeBytes:   int64(len(content)),
		SHA256:      hex.EncodeToString(sum[:]),
		Content:     content,
	}, nil
}
