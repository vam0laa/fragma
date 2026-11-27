//go:build !windows

package main

import (
	"os"
	"strings"
)

func ansiOK() bool { return true }

// replaceFile puts the file at tmp in the place of path, in one step.
var replaceFile = os.Rename

// osLocale: elsewhere the locale variables are all there is.
func osLocale() string { return "en" }

// bypassRunning lists known DPI-bypass processes, since they mask the result.
func bypassRunning() []string {
	var found []string
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		switch n := strings.TrimSpace(string(b)); n {
		case "nfqws", "tpws", "spoofdpi", "ciadpi", "byedpi":
			found = append(found, n)
		}
	}
	return found
}
