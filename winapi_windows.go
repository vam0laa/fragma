package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	wininet  = syscall.NewLazyDLL("wininet.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pInternetSetOption   = wininet.NewProc("InternetSetOptionW")
	pInternetQueryOption = wininet.NewProc("InternetQueryOptionW")
	pGlobalFree          = kernel32.NewProc("GlobalFree")
	pCreateMutex         = kernel32.NewProc("CreateMutexW")
	pOpenMutex           = kernel32.NewProc("OpenMutexW")
	pSetConsoleMode      = kernel32.NewProc("SetConsoleMode")
	pUILanguage          = kernel32.NewProc("GetUserDefaultUILanguage")
	pMessageBox          = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
)

const (
	optSettingsChanged   = 39
	optRefresh           = 37
	optPerConnection     = 75
	perConnFlags         = 1
	perConnProxyServer   = 2
	perConnProxyBypass   = 3
	perConnAutoconfigURL = 4
)

// perConnOption is INTERNET_PER_CONN_OPTIONW; value is a union of a DWORD and
// a string pointer.
type perConnOption struct {
	option uint32
	value  uint64
}

// perConnList is INTERNET_PER_CONN_OPTION_LISTW. A nil connection means the
// normal LAN / Wi-Fi settings.
type perConnList struct {
	size       uint32
	connection *uint16
	count      uint32
	errorIndex uint32
	options    *perConnOption
}

// winProxy reads and writes the settings shown under Settings > Proxy.
type winProxy struct{}

func (winProxy) get() (proxyConfig, error) {
	opts := [4]perConnOption{{option: perConnFlags}, {option: perConnProxyServer}, {option: perConnProxyBypass}, {option: perConnAutoconfigURL}}
	l := perConnList{count: uint32(len(opts)), options: &opts[0]}
	l.size = uint32(unsafe.Sizeof(l))
	n := l.size
	r, _, err := pInternetQueryOption.Call(0, optPerConnection, uintptr(unsafe.Pointer(&l)), uintptr(unsafe.Pointer(&n)))
	runtime.KeepAlive(&opts)
	if r == 0 {
		return proxyConfig{}, fmt.Errorf("reading the proxy settings: %w", err)
	}
	str := func(o *perConnOption) string {
		p := *(**uint16)(unsafe.Pointer(&o.value))
		if p == nil {
			return ""
		}
		defer pGlobalFree.Call(uintptr(o.value)) // Windows allocated it
		var u []uint16
		for q := unsafe.Pointer(p); *(*uint16)(q) != 0; q = unsafe.Add(q, 2) {
			u = append(u, *(*uint16)(q))
		}
		return syscall.UTF16ToString(u)
	}
	c := proxyConfig{Flags: uint32(opts[0].value), Server: str(&opts[1]), Bypass: str(&opts[2]), PAC: str(&opts[3])}
	var errS, errB error
	c.Reg.Server, errS = readRegString(inetKey, "ProxyServer")
	c.Reg.Bypass, errB = readRegString(inetKey, "ProxyOverride")
	if err := errors.Join(errS, errB); err != nil {
		return proxyConfig{}, fmt.Errorf("reading the proxy settings: %w", err)
	}
	c.Reg.Known = true
	return c, nil
}

// readRegString reads one string value of a key under HKCU.
func readRegString(path, name string) (regValue, error) {
	sub, err := syscall.UTF16PtrFromString(strings.TrimPrefix(path, `HKCU\`))
	if err != nil {
		return regValue{}, err
	}
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return regValue{}, err
	}
	var key syscall.Handle
	err = syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, sub, 0, syscall.KEY_QUERY_VALUE, &key)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return regValue{}, nil // a new account may not have the key yet
	}
	if err != nil {
		return regValue{}, fmt.Errorf("%s: %w", path, err)
	}
	defer syscall.RegCloseKey(key)
	var typ, size uint32
	err = syscall.RegQueryValueEx(key, n, nil, &typ, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return regValue{}, nil
	}
	if err != nil {
		return regValue{}, err
	}
	buf := make([]uint16, size/2+1)
	size = uint32(len(buf) * 2)
	if err := syscall.RegQueryValueEx(key, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return regValue{}, err
	}
	if typ != syscall.REG_SZ {
		return regValue{}, fmt.Errorf("%s has an unexpected type", name)
	}
	return regValue{Set: true, Value: syscall.UTF16ToString(buf)}, nil
}

func (winProxy) set(c proxyConfig) error {
	opts := [4]perConnOption{{option: perConnFlags, value: uint64(c.Flags)}, {option: perConnProxyServer}, {option: perConnProxyBypass}, {option: perConnAutoconfigURL}}
	var keep []*uint16
	for i, s := range []string{c.Server, c.Bypass, c.PAC} {
		p, err := syscall.UTF16PtrFromString(s)
		if err != nil {
			return err
		}
		keep = append(keep, p)
		*(**uint16)(unsafe.Pointer(&opts[i+1].value)) = p
	}
	l := perConnList{count: uint32(len(opts)), options: &opts[0]}
	l.size = uint32(unsafe.Sizeof(l))
	r, _, err := pInternetSetOption.Call(0, optPerConnection, uintptr(unsafe.Pointer(&l)), uintptr(l.size))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(&opts)
	if r == 0 {
		return fmt.Errorf("changing the proxy settings: %w", err)
	}
	// Some programs (the Discord updater) read the plain registry values, which
	// Windows does not always bring in line with the settings above.
	// When values read earlier are being put back, they are written as they were.
	values := c.Reg
	if !values.Known {
		values.Server = regValue{Set: c.Server != "", Value: c.Server}
		values.Bypass = regValue{Set: c.Bypass != "", Value: c.Bypass}
	}
	for name, v := range map[string]regValue{"ProxyServer": values.Server, "ProxyOverride": values.Bypass} {
		if v.Set {
			if err := regSet(inetKey, name, v.Value); err != nil {
				return err
			}
		} else {
			reg("delete", inetKey, "/v", name, "/f") // fails if absent
		}
	}
	// tell running programs to read the settings again
	pInternetSetOption.Call(0, optSettingsChanged, 0, 0)
	pInternetSetOption.Call(0, optRefresh, 0, 0)
	return nil
}

const mutexName = `Local\fragma-system-mode`

const inetKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// lockInstance takes the "system mode is running" lock. Windows drops it when
// the process ends, however it ends. ok is false if someone else holds it.
func lockInstance() (release func(), ok bool, err error) {
	name, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, err
	}
	h, _, e := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, false, e
	}
	release = func() { syscall.CloseHandle(syscall.Handle(h)) }
	if errors.Is(e, syscall.ERROR_ALREADY_EXISTS) {
		release()
		return nil, false, nil
	}
	return release, true, nil
}

func instanceRunning() bool {
	name, _ := syscall.UTF16PtrFromString(mutexName)
	const synchronize = 0x00100000
	h, _, _ := pOpenMutex.Call(synchronize, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false
	}
	syscall.CloseHandle(syscall.Handle(h))
	return true
}

// ansiOK switches the console to understand color codes, if it can.
func ansiOK() bool {
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) != nil {
		return false
	}
	const virtualTerminal = 0x4
	r, _, _ := pSetConsoleMode.Call(uintptr(h), uintptr(mode|virtualTerminal))
	return r != 0
}

// osLocale reports the Windows display language, since LANG is usually unset.
func osLocale() string {
	id, _, _ := pUILanguage.Call()
	const turkish = 0x1f
	if id&0x3ff == turkish {
		return "tr"
	}
	return "en"
}

// bypassRunning lists known DPI-bypass processes, since they mask the result.
func bypassRunning() []string {
	out, err := exec.Command("tasklist", "/fo", "csv", "/nh").Output()
	if err != nil {
		return nil
	}
	r := csv.NewReader(strings.NewReader(string(out)))
	r.FieldsPerRecord = -1
	rows, _ := r.ReadAll()
	var found []string
	for _, row := range rows {
		switch n := strings.TrimSuffix(strings.ToLower(row[0]), ".exe"); n {
		case "goodbyedpi", "winws", "spoofdpi", "ciadpi", "byedpi":
			found = append(found, n)
		}
	}

	return found
}

// messageBox shows a warning and waits until it is closed.
func messageBox(title, text string) {
	t, err1 := syscall.UTF16PtrFromString(title)
	m, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	const warningIcon, toFront = 0x30, 0x10000
	pMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), warningIcon|toFront)
}
