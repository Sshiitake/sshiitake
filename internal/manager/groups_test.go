package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestTunnelsInGroup_empty(t *testing.T) {
	cfg := &config.Config{}
	assert.Empty(t, tunnelsInGroup(cfg, "anything"))
}

func TestTunnelsInGroup_filtersByMembership(t *testing.T) {
	cfg := &config.Config{Tunnels: map[string]config.Tunnel{
		"a": {Group: "work"},
		"b": {Group: "personal"},
		"c": {Group: "work"},
	}}
	got := tunnelsInGroup(cfg, "work")
	assert.ElementsMatch(t, []string{"a", "c"}, got)
}

func TestResolveSelectors_groupAndTunnelDedup(t *testing.T) {
	cfg := &config.Config{
		Tunnels: map[string]config.Tunnel{
			"a": {Group: "work"},
			"b": {Group: "work"},
		},
		Groups: map[string]config.Group{"work": {}},
	}
	// Selecting both the group AND a tunnel inside the group should
	// not yield duplicates.
	got, err := resolveSelectors(cfg, []string{"work", "a"})
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, got)
}
