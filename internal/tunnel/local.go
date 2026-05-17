package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// forwardLocal serves ln, dialling each accepted connection through
// sshClient to remoteAddr ("host:port"). It returns when ctx is
// cancelled. Errors that occur while serving a single connection are
// not returned; they are swallowed silently in Phase 1. Phase 2 wires
// a logger in.
func forwardLocal(ctx context.Context, sshClient *ssh.Client, ln net.Listener, remoteAddr string) error {
	var (
		mu     sync.Mutex
		active = make(map[net.Conn]struct{})
	)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		mu.Lock()
		for c := range active {
			_ = c.Close()
		}
		mu.Unlock()
		// Close the SSH client so any pipeOneConn blocked in
		// sshClient.Dial unblocks immediately. Start's defer
		// client.Close() is then a no-op.
		_ = sshClient.Close()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		local, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		mu.Lock()
		active[local] = struct{}{}
		mu.Unlock()
		wg.Add(1)
		go func(local net.Conn) {
			defer wg.Done()
			defer func() {
				mu.Lock()
				delete(active, local)
				mu.Unlock()
			}()
			pipeOneConn(sshClient, local, remoteAddr)
		}(local)
	}
}

func pipeOneConn(sshClient *ssh.Client, local net.Conn, remoteAddr string) {
	defer func() { _ = local.Close() }()
	remote, err := sshClient.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}
