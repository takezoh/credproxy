package bridge

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_forwardsTCPToUnix(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	unixLn, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer unixLn.Close()

	go func() {
		conn, err := unixLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn) //nolint:errcheck
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, "127.0.0.1:0", sockPath)
	}()

	time.Sleep(20 * time.Millisecond)

	var tcpAddr string
	for i := 0; i < 5; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			tcpAddr = ln.Addr().String()
			ln.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = tcpAddr
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

func TestRun_dialFailGraceful(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "missing.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, addr, sockPath) }()

	time.Sleep(20 * time.Millisecond)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Error("expected connection to close when unix socket missing")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run after cancel: %v", err)
	}
	_ = os.Remove(sockPath)
}

func TestForward_bidirectional(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "echo.sock")

	unixLn, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer unixLn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := unixLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn) //nolint:errcheck
	}()

	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpAddr := tcpLn.Addr().String()

	connCh := make(chan net.Conn, 1)
	go func() {
		c, _ := tcpLn.Accept()
		connCh <- c
	}()

	client, err := net.Dial("tcp", tcpAddr)
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer client.Close()

	serverSide := <-connCh
	go forward(serverSide, sockPath)

	msg := []byte("hello")
	if _, err := client.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	client.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echo = %q, want %q", string(buf), string(msg))
	}
}
