package manager

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func startTestSSHServer(t *testing.T) (addr string, hostKey ssh.PublicKey) {
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
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleManagerTestConn(c, cfg)
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func handleManagerTestConn(c net.Conn, cfg *ssh.ServerConfig) {
	defer func() { _ = c.Close() }()
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "")
			continue
		}
		go func(ch ssh.NewChannel) {
			var msg struct {
				DestAddr   string
				DestPort   uint32
				OriginAddr string
				OriginPort uint32
			}
			if err := ssh.Unmarshal(ch.ExtraData(), &msg); err != nil {
				_ = ch.Reject(ssh.ConnectionFailed, "")
				return
			}
			remote, err := net.Dial("tcp", net.JoinHostPort(msg.DestAddr, strconv.Itoa(int(msg.DestPort))))
			if err != nil {
				_ = ch.Reject(ssh.ConnectionFailed, err.Error())
				return
			}
			channel, reqs2, err := ch.Accept()
			if err != nil {
				_ = remote.Close()
				return
			}
			go ssh.DiscardRequests(reqs2)
			go func() { _, _ = io.Copy(channel, remote); _ = channel.Close() }()
			go func() { _, _ = io.Copy(remote, channel); _ = remote.Close() }()
		}(ch)
	}
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}(c)
		}
	}()
	return ln.Addr().String()
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return h, pi
}

func echoHost(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }
func echoPort(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	pi, _ := strconv.Atoi(p)
	return pi
}

func writeTempSSHConfig(t *testing.T, host string, port int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh_config")
	content := fmt.Sprintf("Host %s\n    HostName %s\n    Port %d\n    User tester\n", host, host, port)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
