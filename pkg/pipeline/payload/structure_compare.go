package payload

import (
	"math"
	"math/big"
	"slices"
)

// CheckStructuralCompatibility proves producer guarantees imply consumer
// requirements. Identity never substitutes for a matching literal data path.
func CheckStructuralCompatibility(producer, consumer *Structure, limits StructuralLimits) error {
	b, err := newStructuralBudget(limits)
	if err != nil {
		return err
	}
	for _, s := range []*Structure{producer, consumer} {
		if err := b.schema(s); err != nil {
			return err
		}
	}
	if producer.Representation != consumer.Representation || producer.RootShape != consumer.RootShape {
		return structuralError(StructureEncoding, nil)
	}
	return b.compatible(producer.Root, consumer.Root, nil)
}

func (b *structuralBudget) compatible(p, c *StructuralType, at StructuralLocator) error {
	if err := b.spend(0, 0, 1, at); err != nil {
		return err
	}
	if p.Nullable && !c.Nullable {
		return structuralError(StructureNull, at)
	}
	if p.Kind != c.Kind && (!structuralNumeric(p.Kind) || !structuralNumeric(c.Kind)) {
		return structuralError(StructureType, at)
	}
	if structuralNumeric(p.Kind) {
		if (p.Kind == StructuralNumber && c.Kind != StructuralNumber) ||
			(p.Kind == StructuralDecimal && c.Kind != StructuralDecimal) {
			return structuralError(StructureUnsupported, at)
		}
	}
	if p.Enum != nil {
		for _, member := range p.Enum {
			if err := b.value(c, member, at); err != nil {
				return err
			}
		}
		return nil
	}
	if c.Enum != nil || (c.Format != "" && p.Format != c.Format) ||
		(c.Unique && !p.Unique) || !structuralLengthImplies(p, c) {
		return structuralError(StructureConstraint, at)
	}
	if structuralNumeric(p.Kind) && !structuralNumericImplies(p, c) {
		return structuralError(StructureConstraint, at)
	}
	switch p.Kind {
	case StructuralObject:
		properties := make(map[string]StructuralProperty, len(p.Properties))
		for _, property := range p.Properties {
			properties[property.Name] = property
		}
		consumerNames := make(map[string]bool, len(c.Properties))
		for i, expected := range c.Properties {
			loc := structuralAt(at, i)
			if err := b.spend(0, 0, 1, loc); err != nil {
				return err
			}
			consumerNames[expected.Name] = true
			actual, exists := properties[expected.Name]
			if !exists {
				if expected.Required {
					return structuralError(StructurePresence, loc)
				}
				if p.Additional == StructuralOpen {
					return structuralError(StructureUnsupported, loc)
				}
				continue
			}
			if expected.Required && !actual.Required {
				return structuralError(StructurePresence, loc)
			}
			if err := b.compatible(actual.Type, expected.Type, loc); err != nil {
				return err
			}
		}
		if c.Additional == StructuralClosed {
			if p.Additional != StructuralClosed {
				return structuralError(StructureConstraint, at)
			}
			for _, property := range p.Properties {
				if !consumerNames[property.Name] {
					return structuralError(StructureConstraint, at)
				}
			}
		}
	case StructuralArray, StructuralVector:
		if p.Dimension != c.Dimension {
			return structuralError(StructureConstraint, at)
		}
		return b.compatible(p.Items, c.Items, structuralAt(at, -1))
	case StructuralMap:
		if err := b.compatible(p.Keys, c.Keys, structuralAt(at, -2)); err != nil {
			return err
		}
		return b.compatible(p.Values, c.Values, structuralAt(at, -3))
	default:
		return nil
	}
	return nil
}

func structuralLengthImplies(p, c *StructuralType) bool {
	minimum := 0
	if p.MinLength != nil {
		minimum = *p.MinLength
	}
	return (c.MinLength == nil || minimum >= *c.MinLength) &&
		(c.MaxLength == nil || (p.MaxLength != nil && *p.MaxLength <= *c.MaxLength))
}

func structuralNumericImplies(p, c *StructuralType) bool {
	pmin, pmax := structuralRange(p)
	cmin, cmax := structuralRange(c)
	if pmin.Cmp(cmin) < 0 || pmax.Cmp(cmax) > 0 {
		return false
	}
	if p.Kind == StructuralDecimal {
		return p.Scale <= c.Scale
	}
	if p.Kind == StructuralInteger && c.Kind == StructuralNumber {
		mantissa := 53
		if c.Bits == 32 {
			mantissa = 24
		}
		limit := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(mantissa)))
		return pmin.Cmp(new(big.Rat).Neg(limit)) >= 0 && pmax.Cmp(limit) <= 0
	}
	return p.Kind != StructuralNumber || p.Bits <= c.Bits
}

func structuralRange(t *StructuralType) (*big.Rat, *big.Rat) {
	var minimum, maximum *big.Rat
	switch t.Kind {
	case StructuralInteger:
		minimum, maximum = structuralIntegerRange(t.Bits)
	case StructuralDecimal:
		coefficient := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.Precision)), nil), big.NewInt(1))
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.Scale)), nil)
		maximum = new(big.Rat).SetFrac(coefficient, divisor)
		minimum = new(big.Rat).Neg(maximum)
	case StructuralNumber:
		limit := math.MaxFloat64
		if t.Bits == 32 {
			limit = math.MaxFloat32
		}
		maximum = new(big.Rat).SetFloat64(limit)
		minimum = new(big.Rat).Neg(maximum)
	default:
		return nil, nil
	}
	if t.Min != nil && t.Min.Rat().Cmp(minimum) > 0 {
		minimum = t.Min.Rat()
	}
	if t.Max != nil && t.Max.Rat().Cmp(maximum) < 0 {
		maximum = t.Max.Rat()
	}
	return minimum, maximum
}

// StructuralDeclaration separates revision identity, scoped continuity, and data
// path. These fields are protected inspection data, not safe error text.
type StructuralDeclaration struct {
	Contract, Object, Revision string
	ID, Identity               string
	Path                       StructuralPath
	Locator                    StructuralLocator
	Required                   bool
	Type                       *StructuralType
	matchKey                   string
}

type StructuralMatch struct {
	Before, After *StructuralDeclaration
	PathChanged   bool
}

// MatchStructuralDeclarations supplies revision-impact primitives for known
// consumers. Nil sides mean removal/addition; no external consumer is inferred.
// Explicit IDs match only within contract/object/parent ancestry. ID-less
// declarations match exact unambiguous typed paths, never document positions.
func MatchStructuralDeclarations(before, after *Structure, limits StructuralLimits) ([]StructuralMatch, error) {
	b, err := newStructuralBudget(limits)
	if err != nil {
		return nil, err
	}
	for _, s := range []*Structure{before, after} {
		if err := b.schema(s); err != nil {
			return nil, err
		}
	}
	old, err := b.declarations(before)
	if err != nil {
		return nil, err
	}
	current, err := b.declarations(after)
	if err != nil {
		return nil, err
	}
	index := make(map[string]*StructuralDeclaration, len(current))
	for _, declaration := range current {
		index[declaration.matchKey] = declaration
	}
	result := make([]StructuralMatch, 0, len(old)+len(current))
	used := map[string]bool{}
	for _, declaration := range old {
		if err := b.spend(0, 0, 1, declaration.Locator); err != nil {
			return nil, err
		}
		match := index[declaration.matchKey]
		result = append(result, StructuralMatch{
			Before: declaration, After: match,
			PathChanged: match != nil && !slices.Equal(declaration.Path, match.Path),
		})
		used[declaration.matchKey] = true
	}
	for _, declaration := range current {
		if !used[declaration.matchKey] {
			result = append(result, StructuralMatch{After: declaration})
		}
	}
	return result, nil
}

func (b *structuralBudget) declarations(s *Structure) ([]*StructuralDeclaration, error) {
	var result []*StructuralDeclaration
	var walk func(*StructuralType, StructuralPath, StructuralLocator, []string) error
	walk = func(t *StructuralType, path StructuralPath, at StructuralLocator, parents []string) error {
		for i, property := range t.Properties {
			loc := structuralAt(at, i)
			dataPath := structuralPathAt(path, StructuralPropertySegment, property.Name)
			local := structuralKey([]string{"path", dataPath.String()})
			ancestor := local
			if property.ID != "" {
				local = structuralKey([]string{"id", structuralKey(parents), property.ID})
				ancestor = structuralKey([]string{"id", property.ID})
			}
			key := structuralKey([]string{s.Contract, s.Object, local})
			identity := structuralKey([]string{s.Revision, key})
			if err := b.spend(0, len(identity), 1, loc); err != nil {
				return err
			}
			result = append(result, &StructuralDeclaration{
				Contract: s.Contract, Object: s.Object, Revision: s.Revision,
				ID: property.ID, Identity: identity, Path: dataPath, Locator: loc,
				Required: property.Required, Type: property.Type, matchKey: key,
			})
			ancestry := append(append([]string(nil), parents...), ancestor)
			if err := walk(property.Type, dataPath, loc, ancestry); err != nil {
				return err
			}
		}
		for i, child := range []*StructuralType{t.Items, t.Keys, t.Values} {
			if child != nil {
				kind := []StructuralSegmentKind{StructuralItemSegment, StructuralKeySegment, StructuralValueSegment}[i]
				ancestry := append(append([]string(nil), parents...), kind.String())
				if err := walk(child, structuralPathAt(path, kind, ""), structuralAt(at, -i-1), ancestry); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(s.Root, nil, nil, nil); err != nil {
		return nil, err
	}
	return result, nil
}
