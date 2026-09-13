package main

import (
	"strings"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
)

// nullableSlugText normalizes optional API slugs for text output.
func nullableSlugText(value *api.NullableSlug, fallback string) string {
	if value == nil {
		return fallback
	}
	return slugText(*value, fallback)
}

// slugText trims slugs and substitutes a caller-selected fallback for empty values.
func slugText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// stringText trims arbitrary text metadata and substitutes a fallback for empty values.
func stringText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// utcTimestampText renders API timestamps in stable UTC form for operator text output.
func utcTimestampText(value time.Time, fallback string) string {
	if value.IsZero() {
		return fallback
	}
	return value.UTC().Format(time.RFC3339)
}
