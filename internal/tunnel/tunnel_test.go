package tunnel

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/config"
)

func TestTunnel_dial_connects(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)
	host, port := splitHostPort(t, addr)

	rt := config.ResolvedTunnel{
		Name:      "test",
		SSHHost:   host,
		SSHPort:   port,
		SSHUser:   "tester",
		Type:      config.TypeLocal,
		LocalHost: "127.0.0.1",
		LocalPort: 0,
	}
	opts := Options{
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		DialTimeout:     2 * time.Second,
	}
	tun := New(rt, opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := tun.dial(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, p
}
