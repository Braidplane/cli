package spec

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
)

func normalizeJSONNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized, err := normalizeJSONNumbers(child)
			if err != nil {
				return nil, err
			}
			typed[key] = normalized
		}
	case []any:
		for index, child := range typed {
			normalized, err := normalizeJSONNumbers(child)
			if err != nil {
				return nil, err
			}
			typed[index] = normalized
		}
	case json.Number:
		// Preserve wide integer tokens before conversion while keeping ordinary JSON numbers compatible.
		if !strings.ContainsAny(typed.String(), ".eE") {
			if integer, err := typed.Int64(); err == nil {
				if integer > 1<<53 || integer < -(1<<53) {
					return integer, nil
				}
			} else {
				integer, err := strconv.ParseUint(typed.String(), 10, 64)
				if err != nil {
					return nil, xerror.InvalidInput(xerror.JSON).Detail("pipeline integer exceeds the supported 64-bit range")
				}
				return integer, nil
			}
		}
		fraction, err := typed.Float64()
		if err != nil {
			return nil, xerror.InvalidInput(xerror.JSON).With(err)
		}
		return fraction, nil
	}
	return value, nil
}
