// Package bridge provides a generic TCP↔unix socket forwarder.
// It listens on a TCP address and forwards each connection to a unix socket.
package bridge

import (
	"context"
	"io"
	"log/slog"
	"net"
)

type halfCloser interface {
	CloseWrite() error
}

// Run listens on listenAddr (TCP) and forwards each connection to socketPath (unix).
// Blocks until ctx is cancelled.
func Run(ctx context.Context, listenAddr, socketPath string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	slog.Debug("sockbridge: listening", "tcp", listenAddr, "unix", socketPath)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go forward(conn, socketPath)
	}
}

func forward(tcp net.Conn, socketPath string) {
	unix, err := net.Dial("unix", socketPath)
	if err != nil {
		slog.Warn("sockbridge: dial unix failed", "socket", socketPath, "err", err)
		tcp.Close()
		return
	}
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src) //nolint:errcheck
		if hc, ok := dst.(halfCloser); ok {
			hc.CloseWrite() //nolint:errcheck
		}
		done <- struct{}{}
	}
	go pipe(tcp, unix)
	go pipe(unix, tcp)
	<-done
	<-done
	tcp.Close()
	unix.Close()
}
