//go:build windows

package winsecurefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// File keeps validated ancestor directory handles open until its data handle
// closes, preventing path components from being replaced during admission.
type File struct {
	*os.File
	ancestors  []windows.Handle
	components []pathComponent
	closeOnce  sync.Once
	closeErr   error
}

type pathComponent struct {
	handle windows.Handle
}

// Close closes the data handle and all held ancestor handles.
func (f *File) Close() error {
	f.closeOnce.Do(func() {
		err := f.File.Close()
		for index := len(f.ancestors) - 1; index >= 0; index-- {
			err = errors.Join(err, windows.CloseHandle(f.ancestors[index]))
		}
		f.closeErr = err
	})
	return f.closeErr
}

// Open validates the secret file and its parent/grandparent with the
// confidentiality policy, then returns the same held handle for reading.
func Open(path string) (*File, string, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, "", err
	}
	file, err := openPath(path)
	if err != nil {
		return nil, "", err
	}
	components, err := protectedComponents(file)
	if err != nil {
		file.Close()
		return nil, "", err
	}
	for _, component := range components {
		if err := validateSecretHandle(component.handle, sid); err != nil {
			file.Close()
			return nil, "", err
		}
	}
	return file, sid, nil
}

// OpenManagedImage validates a managed executable and its parent/grandparent
// with the image-integrity policy while retaining every opened ancestor.
func OpenManagedImage(path string) (*File, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	file, err := openPath(path)
	if err != nil {
		return nil, err
	}
	components, err := protectedComponents(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	for _, component := range components {
		if err := validateManagedImageHandle(component.handle, sid); err != nil {
			file.Close()
			return nil, err
		}
	}
	return file, nil
}

// OpenImage opens a process image path without following reparse points. Its
// caller compares the returned file ID with a separately protected managed file.
func OpenImage(path string) (*File, error) {
	return openPath(path)
}

func protectedComponents(file *File) ([]pathComponent, error) {
	if len(file.components) < 3 {
		return nil, fmt.Errorf("protected path needs a file, parent and grandparent")
	}
	return file.components[len(file.components)-3:], nil
}

func validateSecretHandle(handle windows.Handle, current string) error {
	owner, entries, err := securityEntries(handle)
	if err != nil {
		return err
	}
	return validateSecretPolicy(owner, current, entries)
}

func validateManagedImageHandle(handle windows.Handle, current string) error {
	owner, entries, err := securityEntries(handle)
	if err != nil {
		return err
	}
	return validateManagedImagePolicy(owner, current, entries)
}

func securityEntries(handle windows.Handle) (string, []accessACE, error) {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", nil, fmt.Errorf("read protected path security descriptor: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return "", nil, fmt.Errorf("read protected path owner")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return "", nil, fmt.Errorf("read protected path DACL")
	}
	if dacl == nil {
		return owner.String(), nil, nil
	}
	entries := make([]accessACE, 0, dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var raw *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &raw); err != nil {
			return "", nil, err
		}
		header := (*windows.ACE_HEADER)(unsafe.Pointer(raw))
		inheritOnly := header.AceFlags&windows.INHERIT_ONLY_ACE != 0
		var kind aceKind
		switch header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			kind = aceAllowed
		case windows.ACCESS_DENIED_ACE_TYPE:
			kind = aceDenied
		default:
			entries = append(entries, accessACE{kind: aceUnsupported, inheritOnly: inheritOnly})
			continue
		}
		trustee := (*windows.SID)(unsafe.Pointer(&raw.SidStart))
		entries = append(entries, accessACE{
			kind:        kind,
			sid:         trustee.String(),
			mask:        uint32(raw.Mask),
			inheritOnly: inheritOnly,
		})
	}
	return owner.String(), entries, nil
}

func openPath(path string) (*File, error) {
	root, parts, err := pathParts(path)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("protected file path is empty")
	}
	path = root
	ancestors := make([]windows.Handle, 0, len(parts)+1)
	components := make([]pathComponent, 0, len(parts))
	keepAncestors := false
	defer func() {
		if !keepAncestors {
			closeHandles(ancestors)
		}
	}()
	rootHandle, err := openDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("open protected volume root: %w", err)
	}
	ancestors = append(ancestors, rootHandle)
	for index, part := range parts {
		path = filepath.Join(path, part)
		isFile := index == len(parts)-1
		if !isFile {
			handle, err := openDirectory(path)
			if err != nil {
				return nil, err
			}
			ancestors = append(ancestors, handle)
			components = append(components, pathComponent{handle: handle})
			continue
		}
		handle, err := openFinalFile(path)
		if err != nil {
			return nil, err
		}
		components = append(components, pathComponent{handle: handle})
		keepAncestors = true
		return &File{
			File:       os.NewFile(uintptr(handle), path),
			ancestors:  ancestors,
			components: components,
		}, nil
	}
	return nil, fmt.Errorf("protected file path is empty")
}

func openDirectory(path string) (windows.Handle, error) {
	handle, err := openComponent(path, false)
	if err != nil {
		return 0, fmt.Errorf("open protected directory: %w", err)
	}
	info, err := fileInfo(handle)
	if err == nil {
		err = validateAttributes(info, false)
	}
	if err != nil {
		windows.CloseHandle(handle)
		return 0, err
	}
	return handle, nil
}

func openFinalFile(path string) (windows.Handle, error) {
	handle, err := openComponent(path, true)
	if err != nil {
		return 0, fmt.Errorf("open protected file: %w", err)
	}
	info, err := fileInfo(handle)
	if err == nil {
		err = validateAttributes(info, true)
	}
	if err != nil {
		windows.CloseHandle(handle)
		return 0, err
	}
	return handle, nil
}

func closeHandles(handles []windows.Handle) {
	for index := len(handles) - 1; index >= 0; index-- {
		_ = windows.CloseHandle(handles[index])
	}
}

func openComponent(path string, isFile bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL)
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT | windows.FILE_FLAG_BACKUP_SEMANTICS)
	if isFile {
		access = windows.GENERIC_READ | windows.READ_CONTROL
		flags = windows.FILE_FLAG_OPEN_REPARSE_POINT | windows.FILE_FLAG_SEQUENTIAL_SCAN
	}
	// No held component permits write or delete sharing until admission completes.
	return windows.CreateFile(name, access, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, flags, 0)
}

func fileInfo(handle windows.Handle) (*windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func validateAttributes(info *windows.ByHandleFileInformation, isFile bool) error {
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("protected path contains a reparse point")
	}
	isDir := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDir == isFile {
		return fmt.Errorf("protected path component has the wrong file type")
	}
	return nil
}

func pathParts(path string) (string, []string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	if strings.HasPrefix(absolute, `\\`) {
		return "", nil, fmt.Errorf("protected path must be on a local drive")
	}
	volume := filepath.VolumeName(absolute)
	if len(volume) != 2 || volume[1] != ':' {
		return "", nil, fmt.Errorf("protected path must use a local drive letter")
	}
	tail := strings.TrimPrefix(absolute, volume)
	tail = strings.TrimPrefix(tail, string(filepath.Separator))
	parts := strings.Split(tail, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, fmt.Errorf("protected path contains an invalid component")
		}
	}
	return volume + string(filepath.Separator), parts, nil
}

func currentSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read current process SID: %w", err)
	}
	return user.User.Sid.String(), nil
}

// SameFile compares the stable volume and file identifier of two open handles.
func SameFile(a, b *File) (bool, error) {
	left, err := fileInfo(windows.Handle(a.Fd()))
	if err != nil {
		return false, err
	}
	right, err := fileInfo(windows.Handle(b.Fd()))
	if err != nil {
		return false, err
	}
	if left.FileIndexHigh == 0 && left.FileIndexLow == 0 || right.FileIndexHigh == 0 && right.FileIndexLow == 0 {
		return false, fmt.Errorf("stable Windows file identity is unavailable")
	}
	return left.VolumeSerialNumber == right.VolumeSerialNumber && left.FileIndexHigh == right.FileIndexHigh && left.FileIndexLow == right.FileIndexLow, nil
}
