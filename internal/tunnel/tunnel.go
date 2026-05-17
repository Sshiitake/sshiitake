// Package tunnel opens a single SSH tunnel using golang.org/x/crypto/ssh
// and forwards a local port through it.
//
// Phase 1 supports only local forwards; remote and dynamic land in
// Phase 2.
package tunnel

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Sshiitake/sshiitake/internal/config"
)

// Options carries cross-cutting settings shared across tunnels.
type Options struct {
	// HostKeyCallback is required. In production use
	// ssh.InsecureIgnoreHostKey() is forbidden; supply
	// knownhosts.New or ssh.FixedHostKey.
	HostKeyCallback ssh.HostKeyCallback

	// DialTimeout is the maximum time for the initial TCP+SSH handshake.
	// If zero, defaults to 10s.
	DialTimeout time.Duration
}

// Tunnel represents a single configured tunnel. Construct with New,
// then call Start to bring it up.
type Tunnel struct {
	rt   config.ResolvedTunnel
	opts Options
}

// New constructs a Tunnel. It does not connect.
func New(rt config.ResolvedTunnel, opts Options) *Tunnel {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	return &Tunnel{rt: rt, opts: opts}
}

// dial opens an SSH client connection. The caller owns the returned
// client and must Close() it.
func (t *Tunnel) dial(ctx context.Context) (*ssh.Client, error) {
	if t.opts.HostKeyCallback == nil {
		return nil, fmt.Errorf("HostKeyCallback required")
	}
	auth, err := t.buildAuth()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            t.rt.SSHUser,
		Auth:            auth,
		HostKeyCallback: t.opts.HostKeyCallback,
		Timeout:         t.opts.DialTimeout,
	}

	addr := net.JoinHostPort(t.rt.SSHHost, strconv.Itoa(t.rt.SSHPort))

	d := net.Dialer{Timeout: t.opts.DialTimeout}
	tcpConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", addr, err)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, cfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func (t *Tunnel) buildAuth() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try ssh-agent if SSH_AUTH_SOCK is set.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentMethod, err := agentAuth(sock); err == nil {
			methods = append(methods, agentMethod)
		}
	}

	// Try the configured IdentityFile.
	if t.rt.IdentityFile != "" {
		if keyMethod, err := keyAuth(t.rt.IdentityFile); err == nil {
			methods = append(methods, keyMethod)
		}
	}

	// Test servers use NoClientAuth; clients sending an empty Auth list
	// are accepted in that case.
	return methods, nil
}

// agentAuth returns an ssh.AuthMethod backed by the ssh-agent at sock.
func agentAuth(sock string) (ssh.AuthMethod, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	ac := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ac.Signers), nil
}

// keyAuth reads a private key from disk and returns it as an AuthMethod.
func keyAuth(path string) (ssh.AuthMethod, error) {
	if expanded, err := expandHome(path); err == nil {
		path = expanded
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return ssh.PublicKeys(signer), nil
}

func expandHome(p string) (string, error) {
	if len(p) == 0 || p[0] != '~' {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + p[1:], nil
}
