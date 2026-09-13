package must

import "github.com/Braidplane/cli/pkg/slug"

// Fn panics if err is not nil, otherwise returns v.
// Use for functions returning (T, error).
//
// Example:
//
//	slug := must.Fn(slug.New("my-slug"))
func Fn[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// FnErr panics if err is not nil.
// Use for functions returning only error.
//
// Example:
//
//	must.FnErr(req.Config.FromPostgresConfig(...))
func FnErr(err error) {
	if err != nil {
		panic(err)
	}
}

// Slug panics if slug.New returns an error, otherwise returns the parsed slug.
func Slug(value string) slug.Slug {
	return Fn(slug.New(value))
}
