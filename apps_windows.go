package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func systemHosts() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
}

func appsEnabled() bool {
	b, err := os.ReadFile(systemHosts())
	return err == nil && strings.Contains(string(b), hostsBegin)
}

// runApps switches the hosts file entries for programs that ignore the proxy.
// Changing that file needs administrator rights, so Windows asks for them.
func runApps(args []string) {
	if len(args) != 1 || args[0] != "on" && args[0] != "off" && args[0] != "status" {
		fail(T("win.appsusage"))
	}
	on := args[0] == "on"
	path := systemHosts()
	b, err := os.ReadFile(path)
	if err != nil {
		fail(err.Error())
	}
	want := editHosts(string(b), on)
	if args[0] != "status" && want != string(b) {
		_, err = updateHosts(path, on)
		if errors.Is(err, fs.ErrPermission) {
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
			ps := fmt.Sprintf("Start-Process -FilePath %s -ArgumentList 'apps','%s' -Verb RunAs -Wait -WindowStyle Hidden", quote(self()), args[0])
			err = exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
		}
		now, readErr := os.ReadFile(path)
		if err != nil || readErr != nil || string(now) != want {
			msg := T("win.appsfail")
			if err != nil {
				msg += ": " + err.Error()
			}
			fail(msg)
		}
		exec.Command("ipconfig", "/flushdns").Run()
		fmt.Println("  " + T("win.appsbackup", path+hostsBackup))
	}
	if appsEnabled() {
		fmt.Println("  " + T("win.appson"))
	} else {
		fmt.Println("  " + T("win.appsoff"))
	}
}
