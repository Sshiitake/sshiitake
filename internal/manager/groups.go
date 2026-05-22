package manager

import "github.com/Sshiitake/sshiitake/internal/config"

// tunnelsInGroup returns the ordered names of tunnels whose Group field
// matches the given group name.
func tunnelsInGroup(cfg *config.Config, group string) []string {
	var out []string
	for name, t := range cfg.Tunnels {
		if t.Group == group {
			out = append(out, name)
		}
	}
	return out
}
