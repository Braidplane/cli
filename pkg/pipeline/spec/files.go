package spec

import (
	"path/filepath"
	"strings"
)

// IsSupportedFile reports whether path has a supported spec extension.
func IsSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == yamlFileExt || ext == ymlFileExt || ext == jsonFileExt
}

// IsCanonicalFile reports whether path should be considered a committed
// pipeline spec during repository-wide validation.
func IsCanonicalFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".pipeline.yaml") ||
		strings.HasSuffix(base, ".pipeline.yml") ||
		strings.HasSuffix(base, ".pipeline.json")
}
