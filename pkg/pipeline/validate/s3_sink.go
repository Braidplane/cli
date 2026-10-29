package validate

import (
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
)

// S3SinkConfig checks the terminal-sink fields shared by definition validation and runtime.
func S3SinkConfig(config map[string]any) error {
	invalid := func(detail string) error {
		return xerror.InvalidInput(xerror.Pipeline).Detail(detail).WithPublicDetail(detail)
	}
	key, ok := config["key"].(string)
	if !ok || strings.TrimSpace(key) == "" {
		return invalid("sink.config.key is required and must be a non-empty string for s3 backend")
	}
	for _, field := range []string{"bucket", "format", "delimiter", "sheet", "range", "content_type"} {
		if value, exists := config[field]; exists {
			if _, ok := value.(string); !ok {
				return invalid("sink.config." + field + " must be a string for s3 backend")
			}
		}
	}
	for _, field := range []string{"has_header", "overwrite"} {
		if value, exists := config[field]; exists {
			if _, ok := value.(bool); !ok {
				return invalid("sink.config." + field + " must be a bool for s3 backend")
			}
		}
	}
	if value, exists := config["metadata"]; exists {
		switch metadata := value.(type) {
		case map[string]string:
		case map[string]any:
			for _, value := range metadata {
				if _, ok := value.(string); !ok {
					return invalid("sink.config.metadata must be an object with string values for s3 backend")
				}
			}
		default:
			return invalid("sink.config.metadata must be an object with string values for s3 backend")
		}
	}
	return nil
}
