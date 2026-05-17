package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestForwardLocal_passesBytes(t *testing.T) {
	sshAddr, hostKey := newTestSSHServer(t)

	// Echo server: the "remote" service we're tunnelling to.
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	// Open a real SSH client (the same Dial the tunnel package uses).
	clientCfg := &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.FixedHostKey(hostKey),
		Timeout:         2 * time.Second,
	}
	sshClient, err := ssh.Dial("tcp", sshAddr, clientCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sshClient.Close() })

	// Start the local forward.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- forwardLocal(ctx, sshClient, listener, echo.Addr().String())
	}()

	// Connect to the local port, send bytes, expect echo.
	c, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Write([]byte("hello"))
	require.NoError(t, err)
	buf := make([]byte, 5)
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	require.Equal(t, "hello", string(buf))

	// Cancel and confirm forwardLocal returns nil (graceful).
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("forwardLocal did not return after cancel")
	}
}
