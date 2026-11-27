package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// ReplaceFile alone does not keep a file's permissions: the new file's owner stays
// and access entries of both files are merged. For the hosts file that would hand
// ownership, and full access, to whoever ran fragma. So owner, group and access
// list are read before and written back exactly.

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pGetFileSecurity  = advapi32.NewProc("GetFileSecurityW")
	pSetFileSecurity  = advapi32.NewProc("SetFileSecurityW")
	pSDToString       = advapi32.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
	pLookupPrivilege  = advapi32.NewProc("LookupPrivilegeValueW")
	pAdjustPrivileges = advapi32.NewProc("AdjustTokenPrivileges")
	pLocalFree        = kernel32.NewProc("LocalFree")
	pReplaceFile      = kernel32.NewProc("ReplaceFileW")
)

// owner, group and access list
const securityParts = 0x1 | 0x2 | 0x4

// fileSecurity reads a file's security descriptor.
func fileSecurity(path string) ([]byte, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var need uint32
	pGetFileSecurity.Call(uintptr(unsafe.Pointer(name)), securityParts, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 1024
	}
	sd := make([]byte, need)
	r, _, e := pGetFileSecurity.Call(uintptr(unsafe.Pointer(name)), securityParts, uintptr(unsafe.Pointer(&sd[0])), uintptr(len(sd)), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return nil, &os.PathError{Op: "read permissions", Path: path, Err: e}
	}
	return sd, nil
}

// securityText is the descriptor in the SDDL form, to compare and to show.
func securityText(sd []byte) (string, error) {
	var p *uint16
	r, _, e := pSDToString.Call(uintptr(unsafe.Pointer(&sd[0])), 1, securityParts, uintptr(unsafe.Pointer(&p)), 0)
	if r == 0 {
		return "", e
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(p)))
	var u []uint16
	for q := unsafe.Pointer(p); *(*uint16)(q) != 0; q = unsafe.Add(q, 2) {
		u = append(u, *(*uint16)(q))
	}
	return syscall.UTF16ToString(u), nil
}

// enablePrivileges switches on what an administrator needs to give a file back to
// another owner, such as SYSTEM. Without administrator rights this does nothing,
// and setSecurity then fails where the rights would have been needed.
func enablePrivileges() {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return
	}
	var token syscall.Token
	const adjust, query = 0x20, 0x8
	if syscall.OpenProcessToken(process, adjust|query, &token) != nil {
		return
	}
	defer token.Close()
	for _, privilege := range []string{"SeRestorePrivilege", "SeTakeOwnershipPrivilege"} {
		name, err := syscall.UTF16PtrFromString(privilege)
		if err != nil {
			continue
		}
		// TOKEN_PRIVILEGES with one entry
		var tp struct {
			count      uint32
			low        uint32
			high       int32
			attributes uint32
		}
		tp.count, tp.attributes = 1, 0x2 // enabled
		if r, _, _ := pLookupPrivilege.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&tp.low))); r != 0 {
			pAdjustPrivileges.Call(uintptr(token), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
		}
	}
}

// setSecurity gives the file at path the descriptor sd and checks that it took.
func setSecurity(path string, sd []byte) error {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	want, err := securityText(sd)
	if err != nil {
		return err
	}
	enablePrivileges()
	// Without this request Windows forgets that the entries were inherited.
	// The control bits are the second word of a self-relative descriptor.
	c := append([]byte(nil), sd...)
	const autoInheritRequest, autoInherited = 0x0100, 0x0400
	control := uint16(c[2]) | uint16(c[3])<<8
	if control&autoInherited != 0 {
		control |= autoInheritRequest
		c[2], c[3] = byte(control), byte(control>>8)
	}
	if r, _, e := pSetFileSecurity.Call(uintptr(unsafe.Pointer(name)), securityParts, uintptr(unsafe.Pointer(&c[0]))); r == 0 {
		return &os.PathError{Op: "set permissions", Path: path, Err: e}
	}
	now, err := fileSecurity(path)
	if err != nil {
		return err
	}
	got, err := securityText(now)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("permissions of %s are %s, want %s", path, got, want)
	}
	return nil
}

// replaceFile puts the file at tmp in the place of path, in one step, with the
// owner and permissions path has.
var replaceFile = func(tmp, path string) error {
	before, err := fileSecurity(path)
	if err != nil {
		return err
	}
	// Try on the new file first: if the permissions cannot be reproduced, the
	// old file is not touched at all.
	if err := setSecurity(tmp, before); err != nil {
		return fmt.Errorf("cannot keep the permissions, %s was not changed: %w", path, err)
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	from, err := syscall.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	// ReplaceFile also keeps attributes and creation time
	r, _, e := pReplaceFile.Call(uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(from)), 0, 0, 0, 0)
	if r == 0 {
		return &os.PathError{Op: "replace", Path: path, Err: e}
	}
	// it merged the two access lists: write the original one again
	if err := setSecurity(path, before); err != nil {
		return fmt.Errorf("%s was replaced but its permissions could not be put back: %w", path, err)
	}
	return nil
}
