//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/takezoh/credproxy/cmd/credproxyd/admin"
	"github.com/takezoh/credproxy/internal/nativeipc"
	"golang.org/x/sys/windows"
)

const (
	adminChildEnv = "CREDPROXY_NATIVE_ADMIN_CHILD"
	adminReadyEnv = "CREDPROXY_NATIVE_ADMIN_READY"
	adminMarkEnv  = "CREDPROXY_NATIVE_ADMIN_MARKER"
	adminPathEnv  = "CREDPROXY_NATIVE_ADMIN_ENDPOINT"
)

func TestRefreshUsesVerifiedNativeAdminPipe(t *testing.T) {
	if os.Getenv(adminChildEnv) == "1" {
		runNativeAdminChild(t)
		return
	}
	runNativeAdminClient(t)
}

func runNativeAdminChild(t *testing.T) {
	t.Helper()
	endpoint := os.Getenv(adminPathEnv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := admin.Start(ctx, endpoint, fileRefreshRecorder{path: os.Getenv(adminMarkEnv)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(adminReadyEnv), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func runNativeAdminClient(t *testing.T) {
	t.Helper()
	sid, err := nativeipc.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := nativeipc.PipePath("admin", sid)
	if err != nil {
		t.Fatal(err)
	}
	endpoint = fmt.Sprintf("%s-test-%d", endpoint, os.Getpid())
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	protectedExe := protectedTestExecutable(t, source)
	root := t.TempDir()
	ready, marker := filepath.Join(root, "ready"), filepath.Join(root, "refreshed")
	startAdminChild(t, protectedExe, endpoint, ready, marker)
	waitForFile(t, ready)
	args := []string{"--socket", endpoint, "--server-executable", protectedExe, "--all-preloaded"}
	if err := refreshCmd(args); err != nil {
		t.Fatalf("native refresh command: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "called" {
		t.Fatalf("refresh marker = %q, %v", data, err)
	}
}

func startAdminChild(t *testing.T, executable, endpoint, ready, marker string) {
	t.Helper()
	cmd := exec.Command(executable, "-test.run=^TestRefreshUsesVerifiedNativeAdminPipe$")
	cmd.Env = append(os.Environ(), adminChildEnv+"=1", adminPathEnv+"="+endpoint, adminReadyEnv+"="+ready, adminMarkEnv+"="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

func protectedTestExecutable(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	parent, child := filepath.Join(root, "parent"), filepath.Join(root, "parent", "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(child, "credproxyd-test.exe")
	if err := copyFile(source, executable); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{parent, child, executable} {
		setProtectedACL(t, path)
	}
	return executable
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func setProtectedACL(t *testing.T, path string) {
	t.Helper()
	sid, err := nativeipc.CurrentUserSID()
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

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("admin pipe child did not become ready")
}

type fileRefreshRecorder struct{ path string }

func (r fileRefreshRecorder) Refresh(context.Context, []string, bool) error {
	return os.WriteFile(r.path, []byte("called"), 0600)
}
