package filepayload

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"path"
	"strings"
)

const (
	// FilePayloadDigestSHA256 identifies the digest algorithm used for file payload metadata.
	FilePayloadDigestSHA256 = "sha256"

	// FilePayloadDefaultMIMEType is used when a connector cannot determine a safer media type.
	FilePayloadDefaultMIMEType = "application/octet-stream"
)

// FilePayloadMetadata describes an opaque file payload without exposing its raw bytes.
type FilePayloadMetadata struct {
	Filename        string `json:"filename,omitempty"         yaml:"filename,omitempty"`
	MIMEType        string `json:"mime_type"                  yaml:"mime_type"`
	SizeBytes       int64  `json:"size_bytes"                 yaml:"size_bytes"`
	DigestAlgorithm string `json:"digest_algorithm,omitempty" yaml:"digest_algorithm,omitempty"`
	Digest          string `json:"digest,omitempty"           yaml:"digest,omitempty"`
}

// NewFilePayloadMetadata computes stable metadata for an in-memory file payload.
func NewFilePayloadMetadata(filename, mimeType string, content []byte) FilePayloadMetadata {
	sum := sha256.Sum256(content)

	return FilePayloadMetadata{
		Filename:        NormalizeFilePayloadFilename(filename),
		MIMEType:        NormalizeFilePayloadMIMEType(mimeType),
		SizeBytes:       int64(len(content)),
		DigestAlgorithm: FilePayloadDigestSHA256,
		Digest:          hex.EncodeToString(sum[:]),
	}
}

// NormalizeFilePayloadMIMEType strips parameters and falls back to octet-stream when empty.
func NormalizeFilePayloadMIMEType(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType))
	if err != nil || strings.TrimSpace(mediaType) == "" {
		return FilePayloadDefaultMIMEType
	}

	return strings.ToLower(mediaType)
}

// NormalizeFilePayloadFilename reduces untrusted filename hints to a basename.
func NormalizeFilePayloadFilename(filename string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	if normalized == "" {
		return ""
	}

	base := strings.TrimSpace(path.Base(normalized))
	if base == "." || base == "/" || base == ".." {
		return ""
	}

	return base
}

// CloneFilePayloadMetadata returns an independent metadata pointer for mutable containers.
func CloneFilePayloadMetadata(metadata *FilePayloadMetadata) *FilePayloadMetadata {
	if metadata == nil {
		return nil
	}

	cloned := *metadata
	return &cloned
}
