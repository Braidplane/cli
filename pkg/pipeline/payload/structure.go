package payload

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Braidplane/cli/pkg/decimal"
	"github.com/Braidplane/cli/pkg/model"
)

// StructuralKind is deliberately separate from the admitted flat contract syntax.
//
//go:generate -command enumer go tool enumer
//go:generate enumer -type=StructuralKind,StructuralOpenness,StructuralSegmentKind,StructuralCode -json -text -linecomment -output=structure_enumer.go
type StructuralKind int

const (
	StructuralKindUnknown   StructuralKind = iota // unknown
	StructuralObject                              // object
	StructuralArray                               // array
	StructuralMap                                 // map
	StructuralVector                              // vector
	StructuralString                              // string
	StructuralBoolean                             // boolean
	StructuralInteger                             // integer
	StructuralDecimal                             // decimal
	StructuralNumber                              // number
	StructuralDate                                // date
	StructuralLocalTime                           // local_time
	StructuralLocalDatetime                       // local_datetime
	StructuralInstant                             // instant
)

type StructuralOpenness int

const (
	StructuralOpennessUnknown StructuralOpenness = iota // unknown
	StructuralOpen                                      // open
	StructuralClosed                                    // closed
)

// Structure is a resolved, runtime-neutral foundation for inline and ODCS
// compilers and immutable-plan runtime checks. It is not pipeline authoring syntax.
type Structure struct {
	Contract, Revision, Object string
	Representation             model.ContractRepresentation
	RootShape                  model.ContractRootShape
	Root                       *StructuralType
}

type StructuralProperty struct {
	Name, ID string
	Required bool
	Type     *StructuralType
}

type StructuralType struct {
	Kind       StructuralKind
	Nullable   bool
	Properties []StructuralProperty
	Additional StructuralOpenness
	Items      *StructuralType
	Keys       *StructuralType
	Values     *StructuralType
	// Integer bits are signed; number bits are IEEE binary32 or binary64.
	Bits, Precision, Scale, Dimension int
	Min, Max                          *decimal.Value
	MinLength, MaxLength              *int
	Enum                              []any
	Format                            string
	// Unique applies to the immediate array's observed items, not external data.
	Unique bool
}

type StructuralSegmentKind int

const (
	StructuralSegmentKindUnknown StructuralSegmentKind = iota // unknown
	StructuralPropertySegment                                 // property
	StructuralItemSegment                                     // item
	StructuralKeySegment                                      // key
	StructuralValueSegment                                    // value
)

type StructuralSegment struct {
	Kind StructuralSegmentKind
	Name string
}

type StructuralPath []StructuralSegment

// String escapes literal names; ["a.b"] cannot alias ["a"]["b"].
// Paths are for protected inspection, never ordinary diagnostics.
func (p StructuralPath) String() string {
	var out strings.Builder
	out.WriteByte('$')
	for _, segment := range p {
		switch segment.Kind {
		case StructuralSegmentKindUnknown:
			out.WriteString("[?]")
		case StructuralPropertySegment:
			out.WriteByte('[')
			out.WriteString(strconv.Quote(segment.Name))
			out.WriteByte(']')
		case StructuralItemSegment:
			out.WriteString("[*]")
		case StructuralKeySegment:
			out.WriteString("{key}")
		case StructuralValueSegment:
			out.WriteString("{value}")
		}
	}
	return out.String()
}

// StructuralLocator contains only schema positions, never payload keys/values,
// authored names, IDs, or free text. -1/-2/-3 locate items/keys/values.
type StructuralLocator []int

type StructuralCode int

const (
	StructuralCodeUnknown StructuralCode = iota // unknown
	StructureInvalid                            // structure_invalid
	StructureUnsupported                        // structure_unsupported
	StructureLimit                              // structure_limit
	StructureAmbiguous                          // structure_ambiguous
	StructurePresence                           // structure_presence
	StructureNull                               // structure_null
	StructureType                               // structure_type
	StructureConstraint                         // structure_constraint
	StructureEncoding                           // structure_encoding
)

type StructuralError struct {
	Code    StructuralCode
	Locator StructuralLocator
}

func (e *StructuralError) Error() string { return e.Code.String() }

func structuralError(code StructuralCode, at StructuralLocator) error {
	return &StructuralError{Code: code, Locator: append(StructuralLocator(nil), at...)}
}

// Limits may tighten these ceilings. Zero or larger limits fail closed.
type StructuralLimits struct{ Depth, Nodes, Bytes, Work int }

func DefaultStructuralLimits() StructuralLimits {
	return StructuralLimits{Depth: 32, Nodes: 4096, Bytes: 1 << 20, Work: 1_000_000}
}

type structuralBudget struct {
	limits             StructuralLimits
	nodes, bytes, work int
}

func newStructuralBudget(limits StructuralLimits) (*structuralBudget, error) {
	ceilings := DefaultStructuralLimits()
	if limits.Depth <= 0 || limits.Depth > ceilings.Depth || limits.Nodes <= 0 || limits.Nodes > ceilings.Nodes ||
		limits.Bytes <= 0 || limits.Bytes > ceilings.Bytes || limits.Work <= 0 || limits.Work > ceilings.Work {
		return nil, structuralError(StructureLimit, nil)
	}
	return &structuralBudget{limits: limits}, nil
}

func (b *structuralBudget) spend(nodes, size, work int, at StructuralLocator) error {
	if nodes > b.limits.Nodes-b.nodes || size > b.limits.Bytes-b.bytes || work > b.limits.Work-b.work ||
		size > b.limits.Work-b.work-work {
		return structuralError(StructureLimit, at)
	}
	b.nodes += nodes
	b.bytes += size
	b.work += work + size
	return nil
}

func structuralAt(at StructuralLocator, index int) StructuralLocator {
	return append(append(StructuralLocator(nil), at...), index)
}

func structuralPathAt(path StructuralPath, kind StructuralSegmentKind, name string) StructuralPath {
	return append(append(StructuralPath(nil), path...), StructuralSegment{Kind: kind, Name: name})
}

// ValidateStructure rejects the entire tree on unsupported semantics or overflow.
func ValidateStructure(s *Structure, limits StructuralLimits) error {
	b, err := newStructuralBudget(limits)
	if err != nil {
		return err
	}
	return b.schema(s)
}

func (b *structuralBudget) schema(s *Structure) error {
	if s == nil || s.Contract == "" || s.Revision == "" || s.Object == "" ||
		!s.Representation.IsAContractRepresentation() || s.Representation == model.ContractRepresentationUnknown ||
		!s.RootShape.IsAContractRootShape() {
		return structuralError(StructureInvalid, nil)
	}
	if s.Representation == model.ContractRepresentationJSON {
		if s.RootShape == model.ContractRootShapeUnknown {
			return structuralError(StructureInvalid, nil)
		}
	} else if s.RootShape != model.ContractRootShapeUnknown {
		return structuralError(StructureInvalid, nil)
	}
	for _, text := range []string{s.Contract, s.Revision, s.Object} {
		if err := b.spend(0, len(text), 1, nil); err != nil {
			return err
		}
		if !utf8.ValidString(text) {
			return structuralError(StructureInvalid, nil)
		}
	}
	return b.declaration(s.Root, nil, map[*StructuralType]bool{})
}

func (b *structuralBudget) declaration(t *StructuralType, at StructuralLocator, active map[*StructuralType]bool) error {
	if len(at) >= b.limits.Depth {
		return structuralError(StructureLimit, at)
	}
	if err := b.spend(1, 32, 1, at); err != nil {
		return err
	}
	if t == nil || active[t] {
		return structuralError(StructureInvalid, at)
	}
	active[t] = true
	defer delete(active, t)
	if err := b.facets(t, at); err != nil {
		return err
	}
	if len(t.Properties) > b.limits.Nodes-b.nodes {
		return structuralError(StructureLimit, at)
	}
	names, ids := map[string]bool{}, map[string]bool{}
	for i, p := range t.Properties {
		loc := structuralAt(at, i)
		if err := b.spend(0, len(p.Name), 1, loc); err != nil {
			return err
		}
		if err := b.spend(0, len(p.ID), 1, loc); err != nil {
			return err
		}
		if !utf8.ValidString(p.Name) || !utf8.ValidString(p.ID) {
			return structuralError(StructureInvalid, loc)
		}
		if names[p.Name] || (p.ID != "" && ids[p.ID]) {
			return structuralError(StructureAmbiguous, loc)
		}
		names[p.Name], ids[p.ID] = true, true
		if err := b.declaration(p.Type, loc, active); err != nil {
			return err
		}
	}
	for i, child := range []*StructuralType{t.Items, t.Keys, t.Values} {
		if child != nil {
			if err := b.declaration(child, structuralAt(at, -i-1), active); err != nil {
				return err
			}
		}
	}
	return b.rules(t, at)
}

func (b *structuralBudget) facets(t *StructuralType, at StructuralLocator) error {
	invalid := false
	switch t.Kind {
	case StructuralObject:
		if t.Additional != StructuralOpen && t.Additional != StructuralClosed {
			return structuralError(StructureUnsupported, at)
		}
	case StructuralArray:
		invalid = t.Items == nil
	case StructuralMap:
		invalid = t.Keys == nil || t.Keys.Kind != StructuralString || t.Keys.Nullable || t.Values == nil
	case StructuralVector:
		invalid = t.Dimension <= 0 || t.Dimension > b.limits.Nodes || t.Items == nil || t.Items.Kind != StructuralNumber || t.Items.Nullable
	case StructuralInteger:
		invalid = t.Bits != 8 && t.Bits != 16 && t.Bits != 32 && t.Bits != 64
	case StructuralNumber:
		invalid = t.Bits != 32 && t.Bits != 64
	case StructuralDecimal:
		invalid = decimal.ValidateDeclaration(t.Precision, t.Scale) != nil
	case StructuralString, StructuralBoolean, StructuralDate, StructuralLocalTime, StructuralLocalDatetime, StructuralInstant:
	default:
		return structuralError(StructureUnsupported, at)
	}
	invalid = invalid || (t.Kind != StructuralObject && (len(t.Properties) != 0 || t.Additional != StructuralOpennessUnknown)) ||
		(t.Kind != StructuralArray && t.Kind != StructuralVector && t.Items != nil) ||
		(t.Kind != StructuralMap && (t.Keys != nil || t.Values != nil)) ||
		(t.Kind != StructuralVector && t.Dimension != 0) ||
		(t.Kind != StructuralDecimal && (t.Precision != 0 || t.Scale != 0)) ||
		(t.Kind != StructuralInteger && t.Kind != StructuralNumber && t.Bits != 0)
	if invalid {
		return structuralError(StructureInvalid, at)
	}
	return nil
}

func (b *structuralBudget) rules(t *StructuralType, at StructuralLocator) error {
	if err := b.spend(0, len(t.Format), 1, at); err != nil {
		return err
	}
	if t.Format != "" && !structuralFormatSupported(t) {
		return structuralError(StructureUnsupported, at)
	}
	if t.Min != nil || t.Max != nil {
		if !structuralNumeric(t.Kind) {
			return structuralError(StructureUnsupported, at)
		}
		for _, bound := range []*decimal.Value{t.Min, t.Max} {
			if bound != nil {
				if err := b.spend(0, int(bound.EstimatedBytes()), 1, at); err != nil {
					return err
				}
			}
		}
		if t.Min != nil && t.Max != nil && t.Min.Cmp(*t.Max) > 0 {
			return structuralError(StructureInvalid, at)
		}
		minimum, maximum := structuralRange(t)
		if minimum.Cmp(maximum) > 0 {
			return structuralError(StructureInvalid, at)
		}
	}
	if t.MinLength != nil || t.MaxLength != nil {
		if t.Kind != StructuralString && t.Kind != StructuralArray && t.Kind != StructuralMap {
			return structuralError(StructureUnsupported, at)
		}
		if (t.MinLength != nil && *t.MinLength < 0) || (t.MaxLength != nil && *t.MaxLength < 0) ||
			(t.MinLength != nil && t.MaxLength != nil && *t.MinLength > *t.MaxLength) {
			return structuralError(StructureInvalid, at)
		}
	}
	if t.Unique && t.Kind != StructuralArray {
		return structuralError(StructureUnsupported, at)
	}
	if t.Enum != nil {
		if len(t.Enum) == 0 || !structuralScalar(t.Kind) {
			return structuralError(StructureUnsupported, at)
		}
		for _, v := range t.Enum {
			if err := b.valueBudget(v, 0, at); err != nil {
				return err
			}
			// Validate enum members without recursively applying the enum itself.
			memberType := *t
			memberType.Enum = nil
			if err := b.value(&memberType, v, at); err != nil {
				return err
			}
		}
	}
	return nil
}

func structuralFormatSupported(t *StructuralType) bool {
	switch t.Kind {
	case StructuralString:
		return t.Format == "email" || t.Format == "uuid" || t.Format == "url"
	case StructuralDate:
		return t.Format == "yyyy-MM-dd"
	case StructuralLocalTime:
		return t.Format == "HH:mm:ss"
	case StructuralLocalDatetime:
		return t.Format == "yyyy-MM-ddTHH:mm:ss"
	case StructuralInstant:
		return t.Format == "yyyy-MM-ddTHH:mm:ssZ" || t.Format == "YYYY-MM-DDTHH:mm:ss.SSSZ"
	default:
		return false
	}
}

func structuralNumeric(kind StructuralKind) bool {
	return kind == StructuralInteger || kind == StructuralDecimal || kind == StructuralNumber
}

func structuralScalar(kind StructuralKind) bool {
	return kind != StructuralObject && kind != StructuralArray && kind != StructuralMap && kind != StructuralVector
}

// structuralKey encodes tuple boundaries instead of concatenating authored IDs.
func structuralKey(parts []string) string {
	var key strings.Builder
	for _, part := range parts {
		key.WriteString(strconv.Quote(part))
		key.WriteByte(',')
	}
	return key.String()
}
