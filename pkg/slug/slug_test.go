package slug

import (
	"strings"
	"testing"
)

func TestFromName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		wantSlug string
		wantErr  bool
	}{
		{name: "Simple", input: "Admin Role", wantSlug: "admin-role"},
		{name: "Symbols", input: "Admin Role & Co.", wantSlug: "admin-role-co"},
		{name: "TrimHyphens", input: "---Hello---", wantSlug: "hello"},
		{name: "Empty", input: "", wantErr: true},
		{name: "OnlySymbols", input: "***", wantErr: true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := FromName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.wantSlug {
				t.Fatalf("expected %q, got %q", tt.wantSlug, got.String())
			}
		})
	}
}

func TestWithRandomSuffix(t *testing.T) {
	t.Parallel()

	base := Slug("admin-role")
	slugWithSuffix, err := WithRandomSuffix(base, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(slugWithSuffix.String(), "admin-role-") {
		t.Fatalf("expected prefix admin-role-, got %q", slugWithSuffix.String())
	}

	if len(slugWithSuffix.String()) != len("admin-role-")+6 {
		t.Fatalf("unexpected length: %d", len(slugWithSuffix.String()))
	}

	if _, err := New(slugWithSuffix.String()); err != nil {
		t.Fatalf("expected valid slug, got error: %v", err)
	}
}

func TestValidateOrGenerate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		slug         Slug
		generateFrom string
		want         string
		wantErr      bool
	}{
		{
			name:         "ValidSlugReturnsAsIs",
			slug:         Slug("my-role"),
			generateFrom: "Different Name",
			want:         "my-role",
		},
		{
			name:         "EmptySlugGeneratesFromParam",
			slug:         "",
			generateFrom: "Admin Role",
			want:         "admin-role",
		},
		{
			name:         "InvalidSlugGeneratesFromParam",
			slug:         "INVALID-SLUG",
			generateFrom: "Valid Source",
			want:         "valid-source",
		},
		{
			name:         "EmptyParamWithValidSlug",
			slug:         Slug("editor"),
			generateFrom: "",
			want:         "editor",
		},
		{
			name:         "EmptyParamEmptySlugErrors",
			slug:         "",
			generateFrom: "",
			wantErr:      true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.slug.ValidateOrGenerate(tt.generateFrom)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got.String())
			}
		})
	}
}
