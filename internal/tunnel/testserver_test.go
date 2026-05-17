package tunnel

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// newTestSSHServer starts an in-process SSH server on a random
// localhost port. It accepts any client, supports direct-tcpip
// channels, and shuts down at the end of the test.
//
// Returns the listening "host:port" and the host key so tests can
// pin it in their client configs.
func newTestSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
	t.Helper()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(rsaKey)
	require.NoError(t, err)

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)

	go acceptLoop(t, ln, cfg, &wg)

	return ln.Addr().String(), signer.PublicKey()
}

func acceptLoop(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig, wg *sync.WaitGroup) {
	for {
		tcpConn, err := ln.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleConn(t, tcpConn, cfg)
		}()
	}
}

func handleConn(t *testing.T, tcpConn net.Conn, cfg *ssh.ServerConfig) {
	defer tcpConn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "only direct-tcpip is supported")
			continue
		}
		go handleDirectTCPIP(t, ch)
	}
}

// directTCPIPMsg is the payload of a direct-tcpip channel open request,
// per RFC 4254 §7.2.
type directTCPIPMsg struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func handleDirectTCPIP(t *testing.T, ch ssh.NewChannel) {
	var msg directTCPIPMsg
	if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := fmt.Sprintf("%s:%d", msg.DestAddr, msg.DestPort)

	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = ch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	channel, reqs, err := ch.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	go func() {
		_, _ = io.Copy(channel, remote)
		_ = channel.Close()
	}()
	go func() {
		_, _ = io.Copy(remote, channel)
		_ = remote.Close()
	}()
}

// TestNewTestSSHServer verifies the fixture itself: a client should
// be able to open an SSH connection and request a direct-tcpip
// channel that connects to an echo server we start in-process.
func TestNewTestSSHServer(t *testing.T) {
	addr, hostKey := newTestSSHServer(t)

	// Start an echo server the SSH server will connect to.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = echoLn.Close() })
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()

	clientCfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	}
	client, err := ssh.Dial("tcp", addr, clientCfg)
	require.NoError(t, err)
	defer client.Close()

	conn, err := client.Dial("tcp", echoLn.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf))
}
