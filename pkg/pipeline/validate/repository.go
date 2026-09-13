package validate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/xerror"
)

var ErrNoPipelineSpecsFound = xerror.NoPipelineSpecsFound()

// LoadFile reads and validates a single pipeline spec file.
func LoadFile(path string) (*pipelinespec.Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, xerror.Unexpected().Detailf("read %s: %w", path, err)
	}

	pipeline, err := Load(path, data)
	if err != nil {
		return nil, xerror.Wrapf("load %s: %w", path, err)
	}

	return pipeline, nil
}

// Load decodes and validates a pipeline spec from data.
func Load(path string, data []byte) (*pipelinespec.Pipeline, error) {
	pipeline, err := pipelinespec.Load(path, data)
	if err != nil {
		return nil, err
	}
	if err := Validate(pipeline); err != nil {
		return nil, xerror.Wrapf("invalid pipeline %s: %w", path, err)
	}
	if err := ValidateStepInputCompatibility(pipeline); err != nil {
		return nil, xerror.Wrapf("invalid pipeline %s: %w", path, err)
	}
	return pipeline, nil
}

// ValidateDirectory loads all supported pipeline spec files in a directory tree.
// It skips known schema files and returns an aggregated error if any spec is invalid.
func ValidateDirectory(root string) (int, error) {
	count := 0
	errorsByFile := make([]string, 0)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return xerror.Wrapf("walk %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		if !pipelinespec.IsCanonicalFile(path) {
			return nil
		}

		count++
		if _, loadErr := LoadFile(path); loadErr != nil {
			errorsByFile = append(errorsByFile, loadErr.Error())
		}

		return nil
	})
	if walkErr != nil {
		return 0, xerror.Unexpected().Detailf("walk pipeline specs under %s: %w", root, walkErr)
	}

	if count == 0 {
		return 0, xerror.Wrapf("validate directory %s: %w", root, ErrNoPipelineSpecsFound)
	}

	if len(errorsByFile) > 0 {
		return count, xerror.Unexpected().Detailf(
			"validate directory %s: %d invalid spec(s):\n%s",
			root,
			len(errorsByFile),
			strings.Join(errorsByFile, "\n"),
		)
	}

	return count, nil
}
