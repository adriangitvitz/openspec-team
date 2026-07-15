package schema

import "sort"

// BuildOrder is Kahn's algorithm with alphabetical tie-breaks.
func (s *Schema) BuildOrder() []string {
	inDegree := make(map[string]int, len(s.Artifacts))
	dependents := make(map[string][]string, len(s.Artifacts))
	for _, a := range s.Artifacts {
		inDegree[a.ID] = len(a.Requires)
	}
	for _, a := range s.Artifacts {
		for _, req := range a.Requires {
			dependents[req] = append(dependents[req], a.ID)
		}
	}

	var queue []string
	for _, a := range s.Artifacts {
		if inDegree[a.ID] == 0 {
			queue = append(queue, a.ID)
		}
	}
	sort.Strings(queue)

	order := make([]string, 0, len(s.Artifacts))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		var newlyReady []string
		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				newlyReady = append(newlyReady, dep)
			}
		}
		sort.Strings(newlyReady)
		queue = append(queue, newlyReady...)
	}
	return order
}

// NextArtifacts returns the incomplete artifacts whose dependencies are met.
func (s *Schema) NextArtifacts(completed map[string]bool) []string {
	var ready []string
	for _, a := range s.Artifacts {
		if completed[a.ID] {
			continue
		}
		ok := true
		for _, req := range a.Requires {
			if !completed[req] {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, a.ID)
		}
	}
	sort.Strings(ready)
	return ready
}

// IsComplete reports whether every artifact is completed.
func (s *Schema) IsComplete(completed map[string]bool) bool {
	for _, a := range s.Artifacts {
		if !completed[a.ID] {
			return false
		}
	}
	return true
}

// Blocked maps each blocked artifact to its unmet dependencies, sorted.
func (s *Schema) Blocked(completed map[string]bool) map[string][]string {
	blocked := map[string][]string{}
	for _, a := range s.Artifacts {
		if completed[a.ID] {
			continue
		}
		var unmet []string
		for _, req := range a.Requires {
			if !completed[req] {
				unmet = append(unmet, req)
			}
		}
		if len(unmet) > 0 {
			sort.Strings(unmet)
			blocked[a.ID] = unmet
		}
	}
	return blocked
}
