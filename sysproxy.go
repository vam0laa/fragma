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

// proxyConfig is the system proxy configuration of the current user.
type proxyConfig struct {
	Flags  uint32 `json:"flags"`
	Server string `json:"server"`
	Bypass string `json:"bypass"`
	PAC    string `json:"pac"`
}

type proxyBackend interface {
	get() (proxyConfig, error)
	set(proxyConfig) error
}

// savedProxy is written before the system proxy is changed.
type savedProxy struct {
	PID  int         `json:"pid"`  // the process that made the change
	Ours string      `json:"ours"` // the proxy server value it set
	Old  proxyConfig `json:"old"`  // what to put back
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

var errForeignProxy = errors.New("another proxy is already configured")

// ourConfig is the configuration to set. Only HTTPS goes to fragma: it speaks
// nothing but CONNECT, so plain HTTP must keep going direct.
func ourConfig(old proxyConfig, addr string) (proxyConfig, error) {
	if old.Flags&flagPAC != 0 && old.PAC != "" || old.Flags&flagProxy != 0 && old.Server != "" {
		return proxyConfig{}, errForeignProxy
	}
	c := old
	c.Flags |= flagDirect | flagProxy
	c.Server = "https=" + addr
	c.Bypass = localBypass()
	if old.Bypass != "" {
		c.Bypass = old.Bypass + ";" + c.Bypass
	}
	return c, nil
}

func readSaved(path string) (savedProxy, bool, error) {
	var s savedProxy
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, false, fmt.Errorf("%s: %w", path, err)
	}
	return s, true, nil
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
	data, err := json.Marshal(savedProxy{PID: pid, Ours: c.Server, Old: old})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// The old settings must be on disk before anything is changed.
	if err := os.WriteFile(path+".new", data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(path+".new", path); err != nil {
		return err
	}
	if err := b.set(c); err != nil {
		return errors.Join(err, restoreProxy(b, path))
	}
	return nil
}

// restoreProxy puts the saved settings back. If someone else changed the proxy
// in the meantime, their settings are left alone. No saved file: nothing to do.
func restoreProxy(b proxyBackend, path string) error {
	s, ok, err := readSaved(path)
	if err != nil || !ok {
		return err
	}
	cur, err := b.get()
	if err != nil {
		return err
	}
	if cur.Server == s.Ours {
		if err := b.set(s.Old); err != nil {
			return err // keep the file, so it can be tried again
		}
	}
	return os.Remove(path)
}
