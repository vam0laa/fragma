package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// userDir is where per-user files live: ~/.config/fragma on Linux,
// %AppData%\fragma on Windows. Empty if the system has no such place.
func userDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "fragma")
}

func userFile(name string) string {
	if d := userDir(); d != "" {
		return filepath.Join(d, name)
	}
	return ""
}

// appendHosts adds host names to the list file at path, skipping known ones.
func appendHosts(path string, args []string) {
	if len(args) == 0 {
		fail(T("fail.addusage"))
	}
	have := map[string]bool{}
	if f, err := os.Open(path); err == nil {
		for _, h := range parseHosts(f) {
			have[h] = true
		}
		f.Close()
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fail(err.Error())
	}
	defer f.Close()
	for _, a := range args {
		h := normalizeHost(a)
		switch {
		case h == "" || strings.ContainsAny(h, " /:"):
			fail(T("fail.badhost", a))
		case have[h]:
			stepOK(T("step.known"), h)
		default:
			fmt.Fprintln(f, h)
			have[h] = true
			stepOK(T("step.added"), h+" ("+T("d.applies")+")")
		}
	}
}

// printLists shows which hosts fragma changes.
func printLists(h *hostlist, userPath string) {
	b, u, l := h.lists()
	show := func(title string, xs []string, hint string) {
		fmt.Printf("  %s  %s\n", paint(cBold, title), paint(cDim, hint))
		if len(xs) == 0 {
			fmt.Println("    " + paint(cDim, T("list.none")))
		}
		for _, x := range xs {
			fmt.Println("    " + x)
		}
		fmt.Println()
	}
	fmt.Println()
	show(T("list.builtin"), b, T("list.always"))
	show(T("list.yours"), u, userPath)
	show(T("list.learned"), l, T("list.found"))
	fmt.Println("  " + paint(cDim, T("list.footer")))
}

// runAddUser and runListUser work on the per-user files (every system but the
// system-wide Linux service, which keeps its list in /etc/fragma).
func runAddUser(args []string) {
	p := userFile("hosts.txt")
	if p == "" {
		fail(T("fail.nohome"))
	}
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	fs.Parse(args)
	appendHosts(p, fs.Args())
}

func runListUser() {
	printLists(newHostlist(userFile("hosts.txt"), userFile("learned.txt")), userFile("hosts.txt"))
}
