package work

import "fmt"

// ExecutionLocation registers execution identity, not Git state or a second
// repository model. ResourceKeys identify mutually exclusive write resources.
type ExecutionLocation struct {
	IsolatedContextID string   `json:"isolated_context_id,omitempty"`
	ID                string   `json:"id"`
	HostID            string   `json:"host_id"`
	Cwd               string   `json:"cwd"`
	ResourceKeys      []string `json:"resource_keys"`
	Generation        int64    `json:"generation"`
	Revision          int64    `json:"revision"`
}

func (v ExecutionLocation) Validate() error {
	if err := required(v.ID, v.HostID, v.Cwd); err != nil {
		return err
	}
	if err := positive("location generation", v.Generation); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, key := range v.ResourceKeys {
		if err := required(key); err != nil {
			return err
		}
		if seen[key] {
			return fmt.Errorf("duplicate location resource")
		}
		seen[key] = true
	}
	return positive("location revision", v.Revision)
}
