// Package graph owns normalized pipeline DAG semantics.
package graph

import (
	"fmt"
	"sort"
	"strings"

	pipelineruntime "github.com/Braidplane/cli/pkg/pipeline/runtime"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

const (
	refPrefix = "$steps."

	nodeKindInput     = "input"
	nodeKindQuery     = "query"
	nodeKindTransform = "transform"
	nodeKindSink      = "sink"
)

var (
	ErrNilPipeline = xerror.InvalidRequired(xerror.Pipeline)
	ErrOrphanStep  = xerror.OrphanStep()
)

// EdgeKind identifies the spec construct that creates a data-flow edge.
type EdgeKind string

const (
	EdgeKindStepInput      EdgeKind = "step_input"
	EdgeKindQueryParameter EdgeKind = "query_parameter"
)

// Node is one pipeline step in the normalized graph.
type Node struct {
	Step  pipelinespec.Step
	Index int
}

// Edge is a normalized directional data-flow edge between two steps.
type Edge struct {
	Kind         EdgeKind
	FromStepSlug slug.Slug
	ToStepSlug   slug.Slug
	FromIndex    int
	ToIndex      int
	InputName    string
}

// Slug returns the stable edge identifier used by diagnostics.
func (e Edge) Slug() string {
	return fmt.Sprintf("%s->%s", e.FromStepSlug, e.ToStepSlug)
}

// Graph is the normalized pipeline DAG.
type Graph struct {
	nodes               []Node
	stepsBySlug         map[slug.Slug]Node
	edges               []Edge
	stepInputEdges      []Edge
	queryParameterEdges []Edge
	topologicalSteps    []pipelinespec.Step
}

// Normalize builds a normalized DAG from the pipeline spec.
func Normalize(pipeline *pipelinespec.Pipeline) (*Graph, error) {
	if pipeline == nil {
		return nil, ErrNilPipeline
	}

	normalized := &Graph{
		nodes:       make([]Node, 0, len(pipeline.Steps)),
		stepsBySlug: make(map[slug.Slug]Node, len(pipeline.Steps)),
		edges:       make([]Edge, 0, len(pipeline.Steps)*2),
	}

	for index, step := range pipeline.Steps {
		if _, exists := normalized.stepsBySlug[step.Slug]; exists {
			return nil, xerror.Unexpected().Detailf("duplicate step slug %q", step.Slug)
		}

		node := Node{Step: step, Index: index}
		normalized.nodes = append(normalized.nodes, node)
		normalized.stepsBySlug[step.Slug] = node
	}

	for index, step := range pipeline.Steps {
		if err := normalized.normalizeStepInputEdges(pipeline.Steps, index, step); err != nil {
			return nil, err
		}
		if err := normalized.normalizeQueryParameterEdges(pipeline.Steps, index, step); err != nil {
			return nil, err
		}
	}

	topologicalSteps, err := normalized.topologicalOrder()
	if err != nil {
		return nil, err
	}
	normalized.topologicalSteps = topologicalSteps
	normalized.sortEdgesByTopologicalOrder()

	return normalized, nil
}

// ValidateConnectivity requires one weakly connected data-flow component.
func ValidateConnectivity(g *Graph) error {
	if g == nil {
		return nil
	}
	if len(g.nodes) == 1 && g.nodes[0].Step.Input != nil {
		return ErrOrphanStep.Detailf(
			"step %q is not connected to any upstream or downstream step",
			g.nodes[0].Step.Slug,
		)
	}
	if len(g.nodes) <= 1 {
		return nil
	}

	neighbors := make([][]int, len(g.nodes))
	for _, edge := range g.edges {
		neighbors[edge.FromIndex] = append(neighbors[edge.FromIndex], edge.ToIndex)
		neighbors[edge.ToIndex] = append(neighbors[edge.ToIndex], edge.FromIndex)
	}

	visited := make([]bool, len(g.nodes))
	groups := make([]string, 0)
	for i := range g.nodes {
		if visited[i] {
			continue
		}
		visited[i] = true
		pending := []int{i}
		members := make([]int, 0)
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			members = append(members, current)
			for _, next := range neighbors[current] {
				if !visited[next] {
					visited[next] = true
					pending = append(pending, next)
				}
			}
		}
		sort.Ints(members)
		names := make([]string, 0, len(members))
		for _, index := range members {
			names = append(names, fmt.Sprintf("%q", g.nodes[index].Step.Slug))
		}
		groups = append(groups, "["+strings.Join(names, ", ")+"]")
	}
	if len(groups) > 1 {
		detail := fmt.Sprintf(
			"pipeline has %d disconnected data-flow groups: %s; split independent flows into separate pipelines or connect them with an actual data dependency",
			len(groups), strings.Join(groups, "; "),
		)
		return ErrOrphanStep.Detail(detail).WithPublicDetail(detail)
	}

	return nil
}

// Nodes returns steps in spec order.
func (g *Graph) Nodes() []Node {
	if g == nil {
		return nil
	}
	return append([]Node(nil), g.nodes...)
}

// Step returns a step by slug.
func (g *Graph) Step(stepSlug slug.Slug) (pipelinespec.Step, bool) {
	if g == nil {
		return pipelinespec.Step{}, false
	}
	node, ok := g.stepsBySlug[stepSlug]
	return node.Step, ok
}

// Edges returns all normalized data-flow edges.
func (g *Graph) Edges() []Edge {
	if g == nil {
		return nil
	}
	return append([]Edge(nil), g.edges...)
}

// StepInputEdges returns transform and sink input edges only.
func (g *Graph) StepInputEdges() []Edge {
	if g == nil {
		return nil
	}
	return append([]Edge(nil), g.stepInputEdges...)
}

// QueryParameterEdges returns query parameter reference edges only.
func (g *Graph) QueryParameterEdges() []Edge {
	if g == nil {
		return nil
	}
	return append([]Edge(nil), g.queryParameterEdges...)
}

// TopologicalSteps returns the normalized execution order.
func (g *Graph) TopologicalSteps() []pipelinespec.Step {
	if g == nil {
		return nil
	}
	return append([]pipelinespec.Step(nil), g.topologicalSteps...)
}

// TransformInputStepSlugs returns the upstream step slug for each named transform input.
func (g *Graph) TransformInputStepSlugs(stepSlug slug.Slug) (map[string]slug.Slug, bool) {
	step, ok := g.Step(stepSlug)
	if !ok || step.Transform == nil {
		return nil, false
	}

	inputs := make(map[string]slug.Slug, len(step.Transform.Inputs))
	for _, edge := range g.stepInputEdges {
		if edge.ToStepSlug == stepSlug && edge.InputName != "" {
			inputs[edge.InputName] = edge.FromStepSlug
		}
	}

	return inputs, len(inputs) > 0
}

// SinkInputStepSlug returns the upstream step slug for a sink input.
func (g *Graph) SinkInputStepSlug(stepSlug slug.Slug) (slug.Slug, bool) {
	step, ok := g.Step(stepSlug)
	if !ok || step.Sink == nil {
		return "", false
	}

	for _, edge := range g.stepInputEdges {
		if edge.ToStepSlug == stepSlug {
			return edge.FromStepSlug, true
		}
	}

	return "", false
}

func (g *Graph) normalizeStepInputEdges(
	steps []pipelinespec.Step,
	index int,
	step pipelinespec.Step,
) error {
	switch step.Kind {
	case nodeKindTransform:
		if step.Transform == nil {
			return xerror.Unexpected().Detailf(
				"validate step inputs: transform step %q must reference at least one upstream step output",
				step.Slug,
			)
		}
		inputs := sortedTransformInputs(step.Transform)
		if len(inputs) == 0 {
			return xerror.Unexpected().Detailf(
				"validate step inputs: transform step %q must reference at least one upstream step output via inputs",
				step.Slug,
			)
		}
		for _, namedInput := range inputs {
			if namedInput.input.FromStep == "" {
				return xerror.Unexpected().Detailf(
					"validate step inputs: transform step %q input %q must reference an upstream step output",
					step.Slug,
					namedInput.name,
				)
			}

			upstream, ok := findStepBySlug(steps, namedInput.input.FromStep)
			if !ok {
				return xerror.Unexpected().Detailf(
					"validate step inputs: transform step %q input %q must reference step %q",
					step.Slug,
					namedInput.name,
					namedInput.input.FromStep,
				)
			}
			if !isPayloadProducerStep(upstream.Step) {
				return xerror.Unexpected().Detailf(
					"validate step inputs: transform step %q input %q source %q must be a query or transform step, or a declared input step",
					step.Slug,
					namedInput.name,
					namedInput.input.FromStep,
				)
			}

			g.addEdge(Edge{
				Kind:         EdgeKindStepInput,
				FromStepSlug: upstream.Step.Slug,
				ToStepSlug:   step.Slug,
				FromIndex:    upstream.Index,
				ToIndex:      index,
				InputName:    namedInput.name,
			})
		}
	case nodeKindSink:
		if step.Sink == nil {
			return xerror.Unexpected().Detailf(
				"validate step inputs: sink step %q must reference at least one upstream step output",
				step.Slug,
			)
		}
		if step.Sink.Input == nil || step.Sink.Input.FromStep == "" {
			return xerror.Unexpected().Detailf(
				"validate step inputs: sink step %q must reference at least one upstream step output",
				step.Slug,
			)
		}

		upstream, ok := findStepBySlug(steps, step.Sink.Input.FromStep)
		if !ok {
			return xerror.Unexpected().Detailf(
				"validate step inputs: sink step %q must reference step %q",
				step.Slug,
				step.Sink.Input.FromStep,
			)
		}
		if !isPayloadProducerStep(upstream.Step) {
			return xerror.Unexpected().Detailf(
				"validate step inputs: sink step %q input %q must be a query or transform step, or a declared input step",
				step.Slug,
				step.Sink.Input.FromStep,
			)
		}

		g.addEdge(Edge{
			Kind:         EdgeKindStepInput,
			FromStepSlug: upstream.Step.Slug,
			ToStepSlug:   step.Slug,
			FromIndex:    upstream.Index,
			ToIndex:      index,
		})
	}

	return nil
}

func (g *Graph) normalizeQueryParameterEdges(
	steps []pipelinespec.Step,
	index int,
	step pipelinespec.Step,
) error {
	if step.Query == nil {
		return nil
	}

	for _, referencedStepSlug := range collectReferencedStepSlugs(step.Query.Parameters) {
		upstream, ok := findStepBySlug(steps, referencedStepSlug)
		if !ok {
			return xerror.Unexpected().Detailf(
				"validate query parameters: step %q must reference step %q",
				step.Slug,
				referencedStepSlug,
			)
		}
		if !isReferenceProducerStep(upstream.Step) {
			return xerror.Unexpected().Detailf(
				"validate query parameters: step %q reference %q must be a query or transform step",
				step.Slug,
				referencedStepSlug,
			)
		}

		g.addEdge(Edge{
			Kind:         EdgeKindQueryParameter,
			FromStepSlug: upstream.Step.Slug,
			ToStepSlug:   step.Slug,
			FromIndex:    upstream.Index,
			ToIndex:      index,
		})
	}

	return nil
}

func (g *Graph) addEdge(edge Edge) {
	g.edges = append(g.edges, edge)
	switch edge.Kind {
	case EdgeKindStepInput:
		g.stepInputEdges = append(g.stepInputEdges, edge)
	case EdgeKindQueryParameter:
		g.queryParameterEdges = append(g.queryParameterEdges, edge)
	}
}

func (g *Graph) topologicalOrder() ([]pipelinespec.Step, error) {
	indegree := make(map[slug.Slug]int, len(g.nodes))
	outgoing := make(map[slug.Slug][]Edge, len(g.nodes))
	indexBySlug := make(map[slug.Slug]int, len(g.nodes))
	for _, node := range g.nodes {
		indegree[node.Step.Slug] = 0
		indexBySlug[node.Step.Slug] = node.Index
	}
	for _, edge := range g.edges {
		indegree[edge.ToStepSlug]++
		outgoing[edge.FromStepSlug] = append(outgoing[edge.FromStepSlug], edge)
	}

	ready := make([]slug.Slug, 0, len(g.nodes))
	for _, node := range g.nodes {
		if indegree[node.Step.Slug] == 0 {
			ready = append(ready, node.Step.Slug)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		return indexBySlug[ready[i]] < indexBySlug[ready[j]]
	})

	ordered := make([]pipelinespec.Step, 0, len(g.nodes))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]

		node := g.stepsBySlug[current]
		ordered = append(ordered, node.Step)

		nextEdges := outgoing[current]
		sort.SliceStable(nextEdges, func(i, j int) bool {
			return nextEdges[i].ToIndex < nextEdges[j].ToIndex
		})
		for _, edge := range nextEdges {
			indegree[edge.ToStepSlug]--
			if indegree[edge.ToStepSlug] == 0 {
				ready = append(ready, edge.ToStepSlug)
				sort.Slice(ready, func(i, j int) bool {
					return indexBySlug[ready[i]] < indexBySlug[ready[j]]
				})
			}
		}
	}

	if len(ordered) != len(g.nodes) {
		return nil, xerror.Unexpected().Detail("pipeline graph contains a cycle")
	}

	return ordered, nil
}

func (g *Graph) sortEdgesByTopologicalOrder() {
	orderBySlug := make(map[slug.Slug]int, len(g.topologicalSteps))
	for index, step := range g.topologicalSteps {
		orderBySlug[step.Slug] = index
	}

	// Keep normalized edge lists aligned with execution order even when the spec
	// lists steps before their producers.
	sortEdges := func(edges []Edge) {
		sort.SliceStable(edges, func(i, j int) bool {
			left := edges[i]
			right := edges[j]
			leftFrom := orderBySlug[left.FromStepSlug]
			rightFrom := orderBySlug[right.FromStepSlug]
			if leftFrom != rightFrom {
				return leftFrom < rightFrom
			}
			leftTo := orderBySlug[left.ToStepSlug]
			rightTo := orderBySlug[right.ToStepSlug]
			if leftTo != rightTo {
				return leftTo < rightTo
			}
			if left.InputName != right.InputName {
				return left.InputName < right.InputName
			}
			return left.Slug() < right.Slug()
		})
	}

	sortEdges(g.edges)
	sortEdges(g.stepInputEdges)
	sortEdges(g.queryParameterEdges)
}

type namedStepInput struct {
	name  string
	input pipelinespec.StepInput
}

func sortedTransformInputs(transform *pipelinespec.TransformSpec) []namedStepInput {
	if transform == nil || len(transform.Inputs) == 0 {
		return nil
	}

	names := make([]string, 0, len(transform.Inputs))
	for name := range transform.Inputs {
		names = append(names, name)
	}
	sort.Strings(names)

	inputs := make([]namedStepInput, 0, len(names))
	for _, name := range names {
		inputs = append(inputs, namedStepInput{name: name, input: transform.Inputs[name]})
	}
	return inputs
}

func findStepBySlug(
	steps []pipelinespec.Step,
	stepSlug slug.Slug,
) (Node, bool) {
	for i := range steps {
		if steps[i].Slug == stepSlug {
			return Node{Step: steps[i], Index: i}, true
		}
	}

	return Node{}, false
}

func isPayloadProducerStep(step pipelinespec.Step) bool {
	return step.Kind == nodeKindInput || isReferenceProducerStep(step)
}

func isReferenceProducerStep(step pipelinespec.Step) bool {
	return step.Kind == nodeKindQuery || step.Kind == nodeKindTransform
}

func collectReferencedStepSlugs(params map[string]any) []slug.Slug {
	seen := make(map[slug.Slug]struct{})
	for _, value := range params {
		collectReferencedStepSlugsFromValue(value, seen)
	}
	references := make([]slug.Slug, 0, len(seen))
	for reference := range seen {
		references = append(references, reference)
	}
	sort.Slice(references, func(i, j int) bool {
		return references[i].String() < references[j].String()
	})
	return references
}

func collectReferencedStepSlugsFromValue(value any, seen map[slug.Slug]struct{}) {
	ref, ok := stepRef(value)
	if ok {
		rest := strings.TrimPrefix(ref, refPrefix)
		dotIdx := strings.Index(rest, ".")
		if dotIdx > 0 {
			seen[slug.FromString(rest[:dotIdx])] = struct{}{}
		}
		return
	}

	switch typed := value.(type) {
	case map[string]any:
		for _, nested := range typed {
			collectReferencedStepSlugsFromValue(nested, seen)
		}
	case []any:
		for _, nested := range typed {
			collectReferencedStepSlugsFromValue(nested, seen)
		}
	}
}

func stepRef(value any) (string, bool) {
	if _, ok := value.(pipelineruntime.LiteralString); ok {
		return "", false
	}
	s, ok := value.(string)
	if !ok {
		return "", false
	}
	if !strings.HasPrefix(s, refPrefix) {
		return "", false
	}
	return s, true
}
