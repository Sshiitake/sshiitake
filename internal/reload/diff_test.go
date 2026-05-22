package reload

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// cfg is a tiny helper to keep table-driven tests legible.
func cfg(tunnels map[string]config.Tunnel) *config.Config {
	return &config.Config{Tunnels: tunnels, Groups: map[string]config.Group{}}
}

func mkTun(host string, port int) config.Tunnel {
	return config.Tunnel{
		Host:       host,
		Type:       config.TypeLocal,
		LocalHost:  "127.0.0.1",
		LocalPort:  port,
		RemoteHost: "127.0.0.1",
		RemotePort: 80,
	}
}

func TestDiff_addedOnly(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
	})
	new := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
		"db":  mkTun("h2", 2000),
	})
	p := Diff(old, new)
	assert.Equal(t, []string{"db"}, p.Added)
	assert.Empty(t, p.Removed)
	assert.Empty(t, p.Modified)
}

func TestDiff_removedOnly(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
		"db":  mkTun("h2", 2000),
	})
	new := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
	})
	p := Diff(old, new)
	assert.Equal(t, []string{"db"}, p.Removed)
	assert.Empty(t, p.Added)
	assert.Empty(t, p.Modified)
}

func TestDiff_modifiedOnly(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
	})
	// Same name, different RemotePort.
	tun := mkTun("h1", 1000)
	tun.RemotePort = 8080
	new := cfg(map[string]config.Tunnel{
		"api": tun,
	})
	p := Diff(old, new)
	assert.Equal(t, []string{"api"}, p.Modified)
	assert.Empty(t, p.Added)
	assert.Empty(t, p.Removed)
}

func TestDiff_unchangedIsEmpty(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
		"db":  mkTun("h2", 2000),
	})
	new := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
		"db":  mkTun("h2", 2000),
	})
	p := Diff(old, new)
	assert.True(t, p.Empty())
	assert.Empty(t, p.Added)
	assert.Empty(t, p.Removed)
	assert.Empty(t, p.Modified)
}

func TestDiff_combination(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api":     mkTun("h1", 1000),
		"db":      mkTun("h2", 2000),
		"removed": mkTun("h3", 3000),
	})
	// "api" modified (different port). "db" same. "added" is new.
	apiMod := mkTun("h1", 1000)
	apiMod.LocalPort = 1001
	new := cfg(map[string]config.Tunnel{
		"api":   apiMod,
		"db":    mkTun("h2", 2000),
		"added": mkTun("h4", 4000),
	})
	p := Diff(old, new)
	assert.Equal(t, []string{"added"}, p.Added)
	assert.Equal(t, []string{"removed"}, p.Removed)
	assert.Equal(t, []string{"api"}, p.Modified)
}

func TestDiff_nilOldCfgIsAllAdded(t *testing.T) {
	new := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
		"db":  mkTun("h2", 2000),
	})
	p := Diff(nil, new)
	assert.Equal(t, []string{"api", "db"}, p.Added)
}

func TestDiff_nilNewCfgIsAllRemoved(t *testing.T) {
	old := cfg(map[string]config.Tunnel{
		"api": mkTun("h1", 1000),
	})
	p := Diff(old, nil)
	assert.Equal(t, []string{"api"}, p.Removed)
}

func TestDiff_resultsAreSorted(t *testing.T) {
	// Map iteration order is randomised; the sort guarantees the
	// Manager.Apply loop is deterministic, which matters for test
	// reproducibility and human-readable log output.
	old := cfg(map[string]config.Tunnel{})
	new := cfg(map[string]config.Tunnel{
		"zeta":  mkTun("h", 1),
		"alpha": mkTun("h", 2),
		"mid":   mkTun("h", 3),
	})
	p := Diff(old, new)
	assert.Equal(t, []string{"alpha", "mid", "zeta"}, p.Added)
}
