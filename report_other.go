//go:build !linux && !windows

package main

// collectPlatform has nothing of its own to tell on other systems.
func collectPlatform() platformData { return platformData{name: "unknown", logStatus: "none"} }
