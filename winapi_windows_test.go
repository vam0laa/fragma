package main

import "testing"

// Reading only: tests never change the machine's proxy settings.
func TestReadSystemProxy(t *testing.T) {
	c, err := (winProxy{}).get()
	if err != nil {
		t.Skip("no WinINet settings here:", err)
	}
	t.Logf("flags=%d server=%q pac=%q", c.Flags, c.Server, c.PAC)
}
