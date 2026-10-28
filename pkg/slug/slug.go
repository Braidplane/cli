package slug

import (
	"crypto/rand"
	"database/sql/driver"
	"regexp"
	"strings"

	"github.com/Braidplane/cli/pkg/xerror"
)

// Slug is a string type that represents a URL-safe identifier for entities.
// Slugs are lowercase alphanumeric with hyphens, 1-100 characters.
type Slug string

// slugPattern matches valid slug format: lowercase alphanumeric with hyphens
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const maxLength = 100

// ErrInvalidSlug is returned when a slug fails validation
var ErrInvalidSlug = xerror.InvalidSlugFormat()

// validate checks if a slug string is valid.
func validate(s string) error {
	if len(s) < 1 || len(s) > maxLength {
		return ErrInvalidSlug
	}
	if !slugPattern.MatchString(s) {
		return ErrInvalidSlug
	}
	return nil
}

// FromName generates a slug from a name by lowercasing, replacing separators with hyphens,
// removing non-alphanumeric characters, and trimming to the max length.
func FromName(name string) (Slug, error) {
	if name == "" {
		return "", ErrInvalidSlug
	}

	var b strings.Builder
	b.Grow(len(name))

	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			lastHyphen = false
		} else if !lastHyphen && b.Len() > 0 {
			b.WriteByte('-')
			lastHyphen = true
		}

		if b.Len() >= maxLength {
			break
		}
	}

	slugStr := strings.Trim(b.String(), "-")
	if len(slugStr) > maxLength {
		slugStr = strings.Trim(slugStr[:maxLength], "-")
	}
	if slugStr == "" {
		return "", ErrInvalidSlug
	}
	if err := validate(slugStr); err != nil {
		return "", err
	}

	return Slug(slugStr), nil
}

// WithRandomSuffix appends a random alphanumeric suffix to a base slug.
func WithRandomSuffix(base Slug, length int) (Slug, error) {
	if length <= 0 {
		return "", ErrInvalidSlug
	}

	suffix, err := randomSuffix(length)
	if err != nil {
		return "", err
	}

	maxBaseLen := maxLength - 1 - length
	if maxBaseLen < 1 {
		return "", ErrInvalidSlug
	}

	baseStr := base.String()
	if len(baseStr) > maxBaseLen {
		baseStr = strings.TrimRight(baseStr[:maxBaseLen], "-")
	}
	if baseStr == "" {
		return "", ErrInvalidSlug
	}

	result := baseStr + "-" + suffix
	if err := validate(result); err != nil {
		return "", err
	}

	return Slug(result), nil
}

func randomSuffix(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", xerror.Unexpected().Detailf("read slug random suffix bytes: %w", err)
	}

	for i, b := range buf {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}

	return string(buf), nil
}

// New creates a new Slug from a string, validating the format.
// Returns ErrInvalidSlug if the slug is invalid.
func New(s string) (Slug, error) {
	if err := validate(s); err != nil {
		return "", err
	}
	return Slug(s), nil
}

// String returns the string representation of the slug.
func (s Slug) String() string {
	return string(s)
}

// Value implements the driver.Valuer interface for database operations.
func (s Slug) Value() (driver.Value, error) {
	return string(s), nil
}

// FromPgtype creates a Slug from a pgx pgtype.Text value.
// This is used when receiving values from SQLC-generated code.
func FromString(s string) Slug {
	return Slug(s)
}

// Scan implements the sql.Scanner interface for database operations.
func (s *Slug) Scan(val any) error {
	if val == nil {
		*s = ""
		return nil
	}

	str, ok := val.(string)
	if !ok {
		return xerror.InvalidInput(
			xerror.Slug,
		).With(
			xerror.Unexpected().Detail("slug: cannot scan non-string value"),
		)
	}

	*s = Slug(str)
	return nil
}

// ValidateOrGenerate preserves explicit identifiers and generates omitted ones.
func (s Slug) ValidateOrGenerate(generateFrom string) (Slug, error) {
	if s != "" {
		return New(s.String())
	}
	generated, err := FromName(generateFrom)
	if err == nil {
		return generated, nil
	}
	if strings.TrimSpace(generateFrom) == "" {
		return "", err
	}
	return WithRandomSuffix("resource", 12)
}
