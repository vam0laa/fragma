//go:build !linux

package main

import (
	"fmt"
	"os"
)

// System-wide mode needs nftables, systemd and Linux's connection redirection.
// On other systems only the per-app proxy (fragma proxy) and the probe tools exist.
func unsupported([]string) {
	fmt.Fprintln(os.Stderr, "  "+T("os.unsupported"))
	os.Exit(1)
}

func runDaemon(args []string)    { unsupported(args) }
func runInstall(args []string)   { unsupported(args) }
func runUninstall(args []string) { unsupported(args) }
func runAdd(args []string)       { runAddUser(args) }
func runList()                   { runListUser() }
func runDNS(args []string)       { unsupported(args) }
func runStatus()                 { unsupported(nil) }
func runControl(string)          { unsupported(nil) }
