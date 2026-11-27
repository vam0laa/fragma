package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

var pRtlGetVersion = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")

func windowsVersion() string {
	// OSVERSIONINFOW
	var v struct {
		size, major, minor, build, platform uint32
		csd                                 [128]uint16
	}
	v.size = uint32(unsafe.Sizeof(v))
	if r, _, _ := pRtlGetVersion.Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.build)
}

func collectPlatform() platformData {
	installed, running := exists(installPath()), instanceRunning()
	auto, autoErr := readRegString(runKey, "fragma")
	apps := appsEnabled()
	saved, pending, savedErr := readSaved(stateFile())
	pending = pending || savedErr != nil

	proxy := "unknown"
	if c, err := (winProxy{}).get(); err == nil {
		ours := ""
		if savedErr == nil && pending {
			ours = saved.Ours
		}
		proxy = proxyKind(c, ours)
	}
	d := platformData{
		name: "windows", version: windowsVersion(), hostsEntries: apps,
		state: reportState{Installed: boolp(installed), Running: boolp(running),
			OwnSites: countHosts(userFile("hosts.txt")), LearnedSites: countHosts(userFile("learned.txt"))},
		windows: &windowsInfo{SystemProxy: proxy, StartsAtSignIn: autoErr == nil && auto.Set, AppsEntries: apps,
			RestorePending: pending && !running},
	}
	// how it was started is known from the sign-in entry only
	if autoErr == nil && auto.Set {
		d.state.DNSOverHTTPS = boolp(strings.Contains(auto.Value, "--dns"))
	}
	if running && savedErr == nil && pending {
		d.proxyAddr = saved.Ours
	}
	if installed {
		ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
		defer cancel()
		if out, err := exec.CommandContext(ctx, installPath(), "version").Output(); err == nil {
			d.state.InstalledVersion = versionOf(string(out))
		}
	}
	d.checks = append(windowsChecks(installed, running, *d.windows), sameVersion(d.state.InstalledVersion))
	b, err := os.ReadFile(logFile())
	switch {
	case os.IsNotExist(err) || err == nil && len(b) == 0:
		d.logStatus = "none"
	case err != nil:
		d.logStatus = "not_readable"
	default:
		d.logStatus, d.logLines = "read", strings.Split(string(b), "\n")
	}
	return d
}
