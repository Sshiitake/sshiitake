package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/Sshiitake/sshiitake/internal/metrics"
)

// forwardLocal serves ln, dialling each accepted connection through
// sshClient to remoteAddr ("host:port"). It returns when ctx is
// cancelled. If tracker is non-nil, bytes-in and bytes-out are recorded.
func forwardLocal(ctx context.Context, sshClient *ssh.Client, ln net.Listener, remoteAddr string, tracker *metrics.Tracker) error {
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
			pipeOneConn(sshClient, local, remoteAddr, tracker)
		}(local)
	}
}

func pipeOneConn(sshClient *ssh.Client, local net.Conn, remoteAddr string, tracker *metrics.Tracker) {
	defer func() { _ = local.Close() }()
	remote, err := sshClient.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()

	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(remote, local)
		if tracker != nil {
			tracker.AddBytesOut(n)
		}
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(local, remote)
		if tracker != nil {
			tracker.AddBytesIn(n)
		}
		done <- struct{}{}
	}()
	<-done
}
