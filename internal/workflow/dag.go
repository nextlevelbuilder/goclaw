package workflow

import (
	"fmt"
	"strings"
)

// DAG is a directed acyclic graph of steps. Steps are registered by AddStep
// and executed in dependency order by Run.
type DAG struct {
	name  string
	steps map[string]*Step
	order []string // registration order, used for stable output
}

// NewDAG returns an empty DAG with the given name.
func NewDAG(name string) *DAG {
	return &DAG{
		name:  name,
		steps: make(map[string]*Step),
	}
}

// Name returns the DAG name.
func (d *DAG) Name() string { return d.name }

// AddStep registers a step. It returns an error if the step is invalid or its
// ID is already registered.
func (d *DAG) AddStep(s *Step) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if _, ok := d.steps[s.ID]; ok {
		return fmt.Errorf("workflow: duplicate step ID %q", s.ID)
	}
	d.steps[s.ID] = s
	d.order = append(d.order, s.ID)
	return nil
}

// Steps returns the registered steps in registration order.
func (d *DAG) Steps() []*Step {
	out := make([]*Step, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.steps[id])
	}
	return out
}

// Step returns the step with the given ID, or nil.
func (d *DAG) Step(id string) *Step { return d.steps[id] }

// TopoOrder returns step IDs in topological order (dependencies first) or an
// error if the graph contains a cycle, references an unknown dependency, or
// has a step depending on itself.
func (d *DAG) TopoOrder() ([]string, error) {
	return d.topoOrder(d.steps)
}

// topoOrder is a standalone topological sort over a step map (Kahn's
// algorithm). It is a method so tests can exercise it on partial graphs.
func (d *DAG) topoOrder(steps map[string]*Step) ([]string, error) {
	// indegree counts unprocessed dependencies per step.
	indegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))
	for id, s := range steps {
		for _, dep := range s.Deps {
			if dep == id {
				return nil, fmt.Errorf("workflow: step %q depends on itself", id)
			}
			if _, ok := steps[dep]; !ok {
				return nil, fmt.Errorf("workflow: step %q depends on unknown step %q", id, dep)
			}
			indegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	// Queue nodes with zero indegree in registration order for stable output.
	var queue []string
	for _, id := range d.order {
		if steps[id] == nil {
			continue // not in the passed-in map (partial graph)
		}
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, dep := range dependents[id] {
			indegree[dep]--
			if indegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(order) != len(steps) {
		// Remaining nodes form a cycle.
		var cycle []string
		for _, id := range d.order {
			if steps[id] != nil && indegree[id] > 0 {
				cycle = append(cycle, id)
			}
		}
		return nil, fmt.Errorf("workflow: cycle detected involving steps: %s", strings.Join(cycle, ", "))
	}
	return order, nil
}
