package tui

import (
	"homebase/installer/internal/catalog"
	"homebase/installer/internal/scan"
)

// orderForDependencies returns missing reordered so that any entry whose
// catalog spec depends on another entry (StrategyExternalCmd's
// DependsOn, e.g. a Wazuh password hash depending on its plaintext
// password) comes after that dependency -- so by the time the wizard
// reaches the hash, its password has already been resolved and
// Generate's DependencyResolver lookup succeeds. A dependency that
// isn't itself in the gap set (already defined in vault.yml) needs no
// reordering, it's already resolvable.
func orderForDependencies(missing []scan.Reference) []scan.Reference {
	byName := make(map[string]scan.Reference, len(missing))
	for _, r := range missing {
		byName[r.Name] = r
	}

	order := make([]string, 0, len(missing))
	seen := make(map[string]bool, len(missing))

	var visit func(name string, inStack map[string]bool)
	visit = func(name string, inStack map[string]bool) {
		if seen[name] || inStack[name] {
			return
		}
		r, ok := byName[name]
		if !ok {
			return // not in the gap set -- already resolvable, nothing to order
		}
		inStack[name] = true
		if spec, ok := catalog.Lookup(name); ok && spec.DependsOn != "" {
			visit(spec.DependsOn, inStack)
		}
		delete(inStack, name)
		if !seen[name] {
			seen[name] = true
			order = append(order, r.Name)
		}
	}

	for _, r := range missing {
		visit(r.Name, map[string]bool{})
	}

	out := make([]scan.Reference, 0, len(missing))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}
