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
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Sshiitake/sshiitake/internal/config"
	"github.com/Sshiitake/sshiitake/internal/metrics"
)

// Status describes a tunnel's current state.
type Status int

// Status values reported by a Tunnel as it progresses through its lifecycle.
const (
	StatusDown Status = iota
	StatusConnecting
	StatusUp
	StatusStopping
)

func (s Status) String() string {
	switch s {
	case StatusDown:
		return "down"
	case StatusConnecting:
		return "connecting"
	case StatusUp:
		return "up"
	case StatusStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

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

	mu        sync.Mutex
	status    Status
	localAddr string

	metrics *metrics.Tracker
}

// New constructs a Tunnel. It does not connect.
func New(rt config.ResolvedTunnel, opts Options) *Tunnel {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	return &Tunnel{
		rt:      rt,
		opts:    opts,
		status:  StatusDown,
		metrics: metrics.NewTracker(),
	}
}

// Metrics returns the per-tunnel metrics tracker. Non-nil after New.
func (t *Tunnel) Metrics() *metrics.Tracker { return t.metrics }

// Name returns the configured name of this tunnel.
func (t *Tunnel) Name() string { return t.rt.Name }

// Status returns the current tunnel state.
func (t *Tunnel) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// LocalAddr returns the actual listen address ("host:port") once Start
// has succeeded. Empty before then.
func (t *Tunnel) LocalAddr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localAddr
}

func (t *Tunnel) setStatus(s Status) {
	t.mu.Lock()
	t.status = s
	t.mu.Unlock()
}

func (t *Tunnel) setLocalAddr(a string) {
	t.mu.Lock()
	t.localAddr = a
	t.mu.Unlock()
}

// Start dials the SSH server, opens the local listener, and forwards
// connections. It blocks until ctx is cancelled or a fatal error
// occurs. The `started` channel is closed once the listener is
// accepting connections (use to gate on "tunnel is up" in tests and
// CLI mode).
func (t *Tunnel) Start(ctx context.Context, started chan<- struct{}) error {
	if t.rt.Type != config.TypeLocal {
		return fmt.Errorf("tunnel %q: type %q not supported in Phase 1 (local only)",
			t.rt.Name, t.rt.Type)
	}

	t.setStatus(StatusConnecting)

	client, err := t.dial(ctx)
	if err != nil {
		t.setStatus(StatusDown)
		return fmt.Errorf("dial %s: %w", t.rt.Name, err)
	}
	// Note: client.Close() is handled by forwardLocal's ctx-cancel
	// goroutine (or by the explicit close below if listener setup
	// fails). We still call it defensively after forwardLocal returns;
	// *ssh.Client.Close is idempotent.

	listenAddr := net.JoinHostPort(t.rt.LocalHost, strconv.Itoa(t.rt.LocalPort))
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_ = client.Close()
		t.setStatus(StatusDown)
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	t.setLocalAddr(ln.Addr().String())
	t.setStatus(StatusUp)
	if started != nil {
		close(started)
	}

	err = forwardLocal(ctx, client, ln, t.rt.RemoteAddr, t.metrics)
	// forwardLocal's ctx-cancel goroutine closed the client on cancel.
	// Defensive close is still safe (Close is idempotent on *ssh.Client).
	_ = client.Close()

	t.setStatus(StatusStopping)
	_ = ln.Close()
	t.setStatus(StatusDown)
	t.setLocalAddr("")

	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// dial opens an SSH client connection. The caller owns the returned
// client and must Close() it.
func (t *Tunnel) dial(ctx context.Context) (*ssh.Client, error) {
	if t.opts.HostKeyCallback == nil {
		return nil, fmt.Errorf("HostKeyCallback required")
	}
	if t.rt.ProxyJump != "" {
		return nil, fmt.Errorf("tunnel %q: ProxyJump=%q is not yet supported (Phase 2)",
			t.rt.Name, t.rt.ProxyJump)
	}
	auth, agentConn, err := t.buildAuth()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	// The agent socket is only needed during the SSH handshake; once
	// NewClientConn returns we never call back into it. Closing it here
	// (deferred) prevents a leaked unix-socket conn per tunnel.
	if agentConn != nil {
		defer func() { _ = agentConn.Close() }()
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

// buildAuth returns the SSH auth methods to attempt and, if an
// ssh-agent socket was successfully opened, the underlying net.Conn so
// the caller can Close it once the handshake completes.
//
// Returns an error when at least one auth source was attempted AND
// every attempt failed. Silent empty when nothing was configured (the
// test-server NoClientAuth path relies on this).
func (t *Tunnel) buildAuth() ([]ssh.AuthMethod, net.Conn, error) {
	var methods []ssh.AuthMethod
	var agentConn net.Conn
	var attempted bool
	var firstErr error

	// Try ssh-agent if SSH_AUTH_SOCK is set.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		attempted = true
		conn, err := net.Dial("unix", sock)
		if err == nil {
			ac := agent.NewClient(conn)
			methods = append(methods, ssh.PublicKeysCallback(ac.Signers))
			agentConn = conn
		} else if firstErr == nil {
			firstErr = fmt.Errorf("ssh-agent: %w", err)
		}
	}

	// Try the configured IdentityFile.
	if t.rt.IdentityFile != "" {
		attempted = true
		if keyMethod, err := keyAuth(t.rt.IdentityFile); err == nil {
			methods = append(methods, keyMethod)
		} else if firstErr == nil {
			firstErr = err
		}
	}

	if attempted && len(methods) == 0 {
		return nil, nil, fmt.Errorf("no usable auth methods: %w", firstErr)
	}
	// Test servers use NoClientAuth; clients sending an empty Auth list
	// are accepted in that case.
	return methods, agentConn, nil
}

// keyAuth reads a private key from disk and returns it as an AuthMethod.
// Refuses keys with permissions broader than 0600 (matches OpenSSH).
func keyAuth(path string) (ssh.AuthMethod, error) {
	if expanded, err := expandHome(path); err == nil {
		path = expanded
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat private key: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf("private key permissions too open (%04o); want 0600 or stricter", mode)
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
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
