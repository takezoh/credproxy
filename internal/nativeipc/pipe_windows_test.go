//go:build windows

package nativeipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestPipeListenerCapturesCurrentSID(t *testing.T) {
	path := testPipePath("peer")
	listener, err := ListenPipe(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := acceptAsync(listener)
	client, err := winio.DialPipeContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := clientPipeHandshake(context.Background(), client); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	server := receiveConn(t, accepted)
	defer server.Close()
	identity, ok := server.(interface{ PeerSID() string })
	if !ok {
		t.Fatal("accepted connection has no captured SID")
	}
	want, err := CurrentUserSID()
	if err != nil || !SIDMatches(identity.PeerSID(), want) {
		t.Fatalf("peer SID = %q, want current SID %q (err %v)", identity.PeerSID(), want, err)
	}
}

func TestPipeListenerRejectsWrongSID(t *testing.T) {
	current, err := CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := ListenPipe(testPipePath("wrong-sid"), current+"-999")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := acceptAsync(listener)
	client, err := winio.DialPipeContext(context.Background(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := clientPipeHandshake(context.Background(), client); err == nil {
		t.Fatal("wrong-SID connection completed the native handshake")
	}
	listener.Close()
	select {
	case result := <-accepted:
		if result.err == nil {
			result.conn.Close()
			t.Fatal("wrong-SID connection reached the listener caller")
		}
	case <-time.After(time.Second):
		t.Fatal("listener did not stop after close")
	}
}

func TestPipeHandshakeHasBoundedPrefaceRead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	started := time.Now()
	_, err := authenticatePipeClient(server, "S-1-5-18", 20*time.Millisecond)
	if err == nil {
		t.Fatal("accepted a client that sent no preface")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("preface timeout took %s", elapsed)
	}
}

func TestImpersonatedSIDRejectsMissingIdentity(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if _, err := impersonatedSID(client); err == nil {
		t.Fatal("accepted a connection without a native pipe identity")
	}
}

func TestListenPipeRejectsSquattedFirstInstance(t *testing.T) {
	path := testPipePath("squatted")
	squatter, err := winio.ListenPipe(path, &winio.PipeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer squatter.Close()
	if _, err := ListenPipe(path, ""); err == nil {
		t.Fatal("listener accepted a pipe name already claimed by another instance")
	}
}

func TestDialPipeWrongServerWritesNoBytes(t *testing.T) {
	path := testPipePath("wrong-server")
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := acceptAsync(listener)
	expected := protectedTestImage(t)
	if _, err := DialPipe(context.Background(), path, expected); err == nil {
		t.Fatal("accepted a server running a different executable")
	}
	server := receiveConn(t, accepted)
	defer server.Close()
	server.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1)
	if n, err := server.Read(buffer); n != 0 {
		t.Fatalf("unverified server received %d request bytes", n)
	} else if err == nil {
		t.Fatal("read succeeded without request bytes")
	}
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func acceptAsync(listener net.Listener) <-chan acceptResult {
	result := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept()
		result <- acceptResult{conn: conn, err: err}
	}()
	return result
}

func receiveConn(t *testing.T, result <-chan acceptResult) net.Conn {
	t.Helper()
	select {
	case accepted := <-result:
		if accepted.err != nil {
			t.Fatal(accepted.err)
		}
		return accepted.conn
	case <-time.After(5 * time.Second):
		t.Fatal("named pipe accept timed out")
		return nil
	}
}

func testPipePath(name string) string {
	return fmt.Sprintf(`\\.\pipe\credproxyd-test-%s-%d`, name, os.Getpid())
}

func protectedTestImage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(child, "expected.exe")
	if err := os.WriteFile(image, []byte("not the running image"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{parent, child, image} {
		protectTestACL(t, path)
	}
	return image
}

func protectTestACL(t *testing.T, path string) {
	t.Helper()
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}
