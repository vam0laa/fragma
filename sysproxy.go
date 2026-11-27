package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// This file is the logic of the Windows system mode: what to set as the system
// proxy and how to put the old settings back. It has no Windows calls, so it is
// tested on every system with a fake backend.

// Flags of a Windows (WinINet) proxy configuration.
const (
	flagDirect = 0x1
	flagProxy  = 0x2
	flagPAC    = 0x4
)

// regValue is a registry string that may be absent.
type regValue struct {
	Set   bool   `json:"set"`
	Value string `json:"value"`
}

// registryValues are the plain registry values some programs read. Known is false
// when they were not read; then they are written to match Server and Bypass.
type registryValues struct {
	Known  bool     `json:"known"`
	Server regValue `json:"server"`
	Bypass regValue `json:"bypass"`
}

// proxyConfig is the system proxy configuration of the current user.
type proxyConfig struct {
	Flags  uint32         `json:"flags"`
	Server string         `json:"server"`
	Bypass string         `json:"bypass"`
	PAC    string         `json:"pac"`
	Reg    registryValues `json:"reg"`
}

type proxyBackend interface {
	get() (proxyConfig, error)
	set(proxyConfig) error
}

// savedProxy is written before the system proxy is changed.
type savedProxy struct {
	PID  int         `json:"pid"`  // the process that made the change
	Ours string      `json:"ours"` // the proxy server value it set
	Set  proxyConfig `json:"set"`  // all it set, to tell later changes apart
	Old  proxyConfig `json:"old"`  // what to put back
}

// proxyWarn tells the user about settings fragma could not put back as they were.
var proxyWarn = func(key string, args ...any) {
	fmt.Fprintln(os.Stderr, "  ! "+T(key, args...))
}

// localBypass keeps local and LAN addresses away from the proxy, like the
// Linux rules do.
func localBypass() string {
	l := []string{"localhost", "127.*", "10.*", "192.168.*", "169.254.*"}
	for i := 16; i <= 31; i++ {
		l = append(l, fmt.Sprintf("172.%d.*", i))
	}
	return strings.Join(append(l, "<local>"), ";")
}

// withoutLocalBypass takes the entries fragma appended off a bypass list.
func withoutLocalBypass(s string) string {
	if s == localBypass() {
		return ""
	}
	return strings.TrimSuffix(s, ";"+localBypass())
}

var errForeignProxy = errors.New("another proxy is already configured")

// ourConfig is the configuration to set. The server is written as a bare
// address: the Discord updater cannot read the "https=host:port" form.
func ourConfig(old proxyConfig, addr string) (proxyConfig, error) {
	if old.Flags&flagPAC != 0 && old.PAC != "" || old.Flags&flagProxy != 0 && old.Server != "" {
		return proxyConfig{}, errForeignProxy
	}
	c := old
	c.Reg = registryValues{}
	c.Flags |= flagDirect | flagProxy
	c.Server = addr
	c.Bypass = localBypass()
	if old.Bypass != "" {
		c.Bypass = old.Bypass + ";" + c.Bypass
	}
	return c, nil
}

var errDamaged = errors.New("the saved proxy settings are damaged")

func parseSaved(path string) (savedProxy, error) {
	var s savedProxy
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil || s.Ours == "" {
		return savedProxy{}, fmt.Errorf("%s: %w", path, errDamaged)
	}
	return s, nil
}

// readSaved reads the saved settings, from the second copy if the first one is
// damaged. ok is false when there is nothing saved.
func readSaved(path string) (savedProxy, bool, error) {
	s, err := parseSaved(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, false, nil
	}
	if errors.Is(err, errDamaged) {
		if second, e := parseSaved(path + ".bak"); e == nil {
			return second, true, nil
		}
	}
	return s, err == nil, err
}

func writeSaved(path string, s savedProxy) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// the copy first: whenever the file itself exists, so does a complete copy
	if err := writeFileAtomic(path+".bak", data, 0o600); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// enableProxy saves the current settings to path and points HTTPS at addr.
// Settings left over from a run that never cleaned up are restored first.
func enableProxy(b proxyBackend, path, addr string, pid int) error {
	if err := restoreProxy(b, path); err != nil {
		return err
	}
	old, err := b.get()
	if err != nil {
		return err
	}
	c, err := ourConfig(old, addr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// The old settings must be on disk before anything is changed.
	s := savedProxy{PID: pid, Ours: c.Server, Set: c, Old: old}
	if err := writeSaved(path, s); err != nil {
		return err
	}
	if err := b.set(c); err != nil {
		return errors.Join(err, restoreProxy(b, path))
	}
	// Remember the settings as the system reports them, which may differ in form.
	if now, err := b.get(); err == nil && now.Server == c.Server {
		s.Set = now
		return writeSaved(path, s)
	}
	return nil
}

// restored is the configuration to go back to: the old one, except for the parts
// someone else changed while fragma's proxy was set.
func restored(cur proxyConfig, s savedProxy) proxyConfig {
	c := s.Old
	if s.Set.Server == "" { // written by 0.4.0, which did not record what it set
		return c
	}
	if cur.Bypass != s.Set.Bypass {
		c.Bypass = withoutLocalBypass(cur.Bypass)
		c.Reg.Bypass = regValue{Set: c.Bypass != "", Value: c.Bypass}
	}
	if cur.PAC != s.Set.PAC {
		c.PAC = cur.PAC
	}
	if cur.Flags != s.Set.Flags {
		c.Flags = cur.Flags&^flagProxy | s.Old.Flags&flagProxy
	}
	return c
}

// restoreProxy puts the saved settings back. If someone else changed the proxy
// in the meantime, their settings are left alone. No saved file: nothing to do.
func restoreProxy(b proxyBackend, path string) error {
	s, ok, err := readSaved(path)
	if errors.Is(err, errDamaged) {
		return recoverDamaged(b, path)
	}
	if err != nil || !ok {
		return err
	}
	cur, err := b.get()
	if err != nil {
		return err
	}
	if cur.Server == s.Ours {
		if err := b.set(restored(cur, s)); err != nil {
			return err // keep the file, so it can be tried again
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	os.Remove(path + ".bak")
	return nil
}

// recoverDamaged handles saved settings that cannot be read any more. The old
// settings are lost, but the proxy must not keep pointing at a port nobody serves:
// if the current settings are plainly fragma's, the proxy is turned off. Either
// way the user is told, and the damaged file is put aside.
func recoverDamaged(b proxyBackend, path string) error {
	cur, err := b.get()
	if err != nil {
		return err
	}
	ours := cur.Flags&flagProxy != 0 && strings.HasPrefix(cur.Server, "127.0.0.1:") && strings.HasSuffix(cur.Bypass, localBypass())
	message := "win.statebad"
	if ours {
		off := cur
		off.Flags = cur.Flags&^flagProxy | flagDirect
		off.Server, off.Bypass, off.Reg = "", withoutLocalBypass(cur.Bypass), registryValues{}
		if err := b.set(off); err != nil {
			return err
		}
		message = "win.statelost"
	}
	if err := os.Rename(path, path+".damaged"); err != nil {
		return err
	}
	os.Remove(path + ".bak")
	proxyWarn(message, path+".damaged")
	return nil
}

// proxyKind says in one word whose the system proxy is. ours is the address
// fragma set, if it is known.
func proxyKind(c proxyConfig, ours string) string {
	switch {
	case c.Flags&flagProxy != 0 && c.Server != "" && (c.Server == ours ||
		strings.HasPrefix(c.Server, "127.0.0.1:") && strings.HasSuffix(c.Bypass, localBypass())):
		return "fragma"
	case c.Flags&flagPAC != 0 && c.PAC != "":
		return "setup_script"
	case c.Flags&flagProxy != 0 && c.Server != "":
		return "other"
	}
	return "none"
}

// windowsChecks are the checks that only make sense on Windows.
func windowsChecks(installed, running bool, w windowsInfo) []reportCheck {
	return []reportCheck{
		check("installed", installed, "FR_NOT_INSTALLED"),
		check("running", running, "FR_NOT_RUNNING"),
		warn("starts_by_itself", installed && !w.StartsAtSignIn, "FR_NOT_AT_BOOT"),
		check("leftover_proxy", running || w.SystemProxy != "fragma", "FR_PROXY_LEFT"),
		check("leftover_hosts", running || !w.AppsEntries, "FR_HOSTS_LEFT"),
		check("saved_settings", !w.RestorePending, "FR_RESTORE_PENDING"),
		warn("proxy_in_use", !running && (w.SystemProxy == "other" || w.SystemProxy == "setup_script"), "FR_PROXY_FOREIGN"),
	}
}
