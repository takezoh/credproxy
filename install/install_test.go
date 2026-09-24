package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeGo = `#!/bin/sh
out=""
pkg=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	-o)
		shift
		out="$1"
		;;
	*)
		pkg="$1"
		;;
	esac
	shift
done
printf '%s\n' "$pkg" >> "$GO_CALLS"
printf '#!/bin/sh\nexit 0\n' > "$out"
chmod 0755 "$out"
`

func requireBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh requires a POSIX shell")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
}

func writeFakeGo(t *testing.T, binDir string) {
	t.Helper()
	path := filepath.Join(binDir, "go")
	if err := os.WriteFile(path, []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runInstall(t *testing.T, env []string) (string, string, int) {
	t.Helper()
	script := filepath.Join("..", "install.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = "."
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run install.sh: %v", err)
	}
	return stdout.String(), stderr.String(), code
}

func TestInstallScriptPlacesBinariesOnUserPath(t *testing.T) {
	requireBash(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")
	calls := filepath.Join(root, "go-calls")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeGo(t, binDir)

	_, stderr, code := runInstall(t, []string{
		"HOME=" + home,
		"PATH=" + binDir + ":/usr/bin:/bin",
		"GO_CALLS=" + calls,
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	for _, name := range []string{"credproxyd", "credproxy"} {
		target := filepath.Join(home, ".local", "bin", name)
		info, err := os.Lstat(target)
		if err != nil {
			t.Fatalf("expected installed binary: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s must be a resolved copy, not a symlink", target)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (mode %v)", target, info.Mode())
		}
	}

	recorded, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	got := string(recorded)
	for _, pkg := range []string{"./cmd/credproxyd", "./cmd/credproxy"} {
		if !strings.Contains(got, pkg) {
			t.Errorf("go build was not invoked for %s (calls: %q)", pkg, got)
		}
	}
}

func TestInstallScriptHonoursBindirOverride(t *testing.T) {
	requireBash(t)
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	custom := filepath.Join(root, "custom-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeGo(t, binDir)

	_, stderr, code := runInstall(t, []string{
		"HOME=" + filepath.Join(root, "home"),
		"BINDIR=" + custom,
		"PATH=" + binDir + ":/usr/bin:/bin",
		"GO_CALLS=" + filepath.Join(root, "go-calls"),
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(custom, "credproxyd")); err != nil {
		t.Fatalf("BINDIR override ignored: %v", err)
	}
}

func TestInstallScriptFailsClosedWithoutGo(t *testing.T) {
	requireBash(t)
	root := t.TempDir()
	_, stderr, code := runInstall(t, []string{
		"HOME=" + filepath.Join(root, "home"),
		"PATH=/usr/bin:/bin",
	})
	if code != 2 {
		t.Fatalf("expected exit 2 without go, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "go unavailable") {
		t.Errorf("expected actionable go error, got %q", stderr)
	}
}
