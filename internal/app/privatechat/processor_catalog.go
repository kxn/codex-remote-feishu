package privatechat

import "sort"

// CatalogSnapshot copies location candidates while the private service is
// exclusively held. Filesystem validation belongs outside the processor lock.
func (p *Processor) CatalogSnapshot() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	paths := map[string]bool{}
	for path := range p.app.Service().RecentPersistedWorkspaces(100) {
		if path != "" {
			paths[path] = true
		}
	}
	for _, instance := range p.app.Service().Instances() {
		if instance != nil && instance.WorkspaceRoot != "" {
			paths[instance.WorkspaceRoot] = true
		}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
