//go:build windows

package winsecurefile

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOpenUsesHeldProtectedFileHandle(t *testing.T) {
	path, dirs := protectedTokenPath(t)
	secureACLs(t, append(dirs, path)...)
	file, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	value, err := io.ReadAll(file)
	if err != nil || string(value) != "test-token" {
		t.Fatalf("read held token = %q, %v", value, err)
	}
	if err := os.WriteFile(path, []byte("replaced"), 0600); err == nil {
		t.Fatal("validated token file remained replaceable while its handle was held")
	}
}

func TestOpenHoldsAncestorDirectoryAuthority(t *testing.T) {
	path, dirs := protectedTokenPath(t)
	secureACLs(t, append(dirs, path)...)
	file, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	parent := filepath.Dir(path)
	moved := filepath.Join(filepath.Dir(parent), "moved-parent")
	if err := os.Rename(parent, moved); err == nil {
		t.Fatal("validated parent directory remained replaceable while its handle was held")
	}
}

func TestOpenImageHoldsAncestryThroughIdentityComparison(t *testing.T) {
	root := t.TempDir()
	parent, child := filepath.Join(root, "parent"), filepath.Join(root, "parent", "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(child, "image.exe")
	if err := os.WriteFile(image, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	left, err := OpenImage(image)
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := OpenImage(image)
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	if same, err := SameFile(left, right); err != nil || !same {
		t.Fatalf("same image identity = %v, %v", same, err)
	}
	if err := os.Rename(parent, filepath.Join(root, "moved-parent")); err == nil {
		t.Fatal("image ancestry remained replaceable during identity admission")
	}
}

func TestOpenManagedImageAllowsUntrustedReadExecute(t *testing.T) {
	path, dirs := protectedTokenPath(t)
	secureACLs(t, append(dirs, path)...)
	setTestACL(t, path, "(A;;GRGX;;;WD)")
	file, err := OpenManagedImage(path)
	if err != nil {
		t.Fatalf("open managed image with broad read/execute ACL: %v", err)
	}
	file.Close()
	setTestACL(t, path, "(A;;FA;;;WD)")
	if file, err := OpenManagedImage(path); err == nil {
		file.Close()
		t.Fatal("accepted a managed image with an untrusted full-access ACE")
	}
}

func TestOpenRejectsBroadDACL(t *testing.T) {
	path, dirs := protectedTokenPath(t)
	secureACLs(t, append(dirs, path)...)
	setTestACL(t, path, "(A;;FR;;;WD)")
	if file, _, err := Open(path); err == nil {
		file.Close()
		t.Fatal("accepted a token DACL that grants access to Everyone")
	}
}

func TestOpenRejectsReparsePoint(t *testing.T) {
	path, dirs := protectedTokenPath(t)
	secureACLs(t, append(dirs, path)...)
	link := filepath.Join(filepath.Dir(path), "token-link")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if file, _, err := Open(link); err == nil {
		file.Close()
		t.Fatal("accepted a reparse-point token path")
	}
}

func protectedTokenPath(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child, "token")
	if err := os.WriteFile(path, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	return path, []string{parent, child}
}

func secureACLs(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		setTestACL(t, path, "")
	}
}

func setTestACL(t *testing.T, path, extraACE string) {
	t.Helper()
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	sddl := "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)" + extraACE
	sd, err := windows.SecurityDescriptorFromString(sddl)
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
