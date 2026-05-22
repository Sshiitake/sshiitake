package reload

import (
	"reflect"
	"sort"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// Plan describes the difference between two configs in terms of tunnel
// names that need to be applied to a running Manager.
//
//   - Added: present in new, not in old. Start a fresh tunnel.
//   - Removed: present in old, not in new. Stop the running tunnel.
//   - Modified: present in both with different config. Stop + start.
//
// Slices are sorted for deterministic iteration in Apply.
type Plan struct {
	Added    []string
	Removed  []string
	Modified []string
}

// Empty reports true when no tunnels need touching.
func (p Plan) Empty() bool {
	return len(p.Added) == 0 && len(p.Removed) == 0 && len(p.Modified) == 0
}

// Diff compares two configs and returns the plan to bring the live
// tunnel set from oldCfg to newCfg. Nil inputs are treated as
// empty-config equivalents so callers can compare a freshly-loaded
// config against zero state without special-casing.
//
// Modified detection uses reflect.DeepEqual on the Tunnel struct. The
// Tunnel type is a small flat record so this is cheap and correct.
// Group membership changes do NOT count as modifications: groups are a
// selector concern, not a lifecycle concern.
func Diff(oldCfg, newCfg *config.Config) Plan {
	oldTunnels := map[string]config.Tunnel{}
	newTunnels := map[string]config.Tunnel{}
	if oldCfg != nil {
		oldTunnels = oldCfg.Tunnels
	}
	if newCfg != nil {
		newTunnels = newCfg.Tunnels
	}

	var p Plan
	for name, nt := range newTunnels {
		ot, ok := oldTunnels[name]
		if !ok {
			p.Added = append(p.Added, name)
			continue
		}
		if !reflect.DeepEqual(ot, nt) {
			p.Modified = append(p.Modified, name)
		}
	}
	for name := range oldTunnels {
		if _, ok := newTunnels[name]; !ok {
			p.Removed = append(p.Removed, name)
		}
	}

	sort.Strings(p.Added)
	sort.Strings(p.Removed)
	sort.Strings(p.Modified)
	return p
}
