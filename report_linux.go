//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// linuxSources are the places the Linux part reads, so that tests can replace them.
type linuxSources struct {
	osRelease, kernel, binary, dropIn, hosts, learned string
	run                                               func(name string, args ...string) (stdout, stderr string, err error)
}

func systemSources() linuxSources {
	return linuxSources{
		osRelease: "/etc/os-release", kernel: "/proc/sys/kernel/osrelease",
		binary: binDest, dropIn: dropIn,
		hosts: defaultHostsFile, learned: defaultStateDir + "/learned.txt",
		run: func(name string, args ...string) (string, string, error) {
			var out, errOut bytes.Buffer
			c := exec.Command(name, args...)
			c.Stdout, c.Stderr = &out, &errOut
			err := c.Run()
			return out.String(), errOut.String(), err
		},
	}
}

func collectPlatform() platformData { return collectLinux(systemSources()) }

func collectLinux(s linuxSources) platformData {
	release := map[string]string{}
	if b, err := os.ReadFile(s.osRelease); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(l, "="); ok {
				release[k] = strings.Trim(v, `"'`)
			}
		}
	}
	kernel, _ := os.ReadFile(s.kernel)

	// not every user has the sbin folders in PATH
	nftVersion := ""
	for _, name := range []string{"nft", "/usr/sbin/nft"} {
		if out, _, err := s.run(name, "--version"); err == nil {
			nftVersion = numberIn(out)
			break
		}
	}
	says := func(args ...string) bool { _, _, err := s.run("systemctl", args...); return err == nil }
	installed := exists(s.binary)
	running := says("is-active", "--quiet", "fragma")
	enabled := says("is-enabled", "--quiet", "fragma")

	installedVersion := ""
	if installed {
		if out, _, err := s.run(s.binary, "version"); err == nil {
			installedVersion = versionOf(out)
		}
	}
	dnsOn := boolp(exists(s.dropIn))
	// 0.1 had no such switch: whether it used DNS over HTTPS cannot be told here
	if strings.HasPrefix(installedVersion, "0.1.") {
		dnsOn = nil
	}

	d := platformData{
		name: token(release["ID"]), version: release["VERSION_ID"],
		state: reportState{Installed: boolp(installed), Running: boolp(running), DNSOverHTTPS: dnsOn, InstalledVersion: installedVersion,
			OwnSites: countHosts(s.hosts), LearnedSites: countHosts(s.learned)},
		linux: &linuxInfo{Kernel: numberIn(string(kernel)), Nftables: nftVersion, ServiceEnabled: boolp(enabled)},
	}
	if d.version != "" { // rolling distributions have none
		if !releaseNumber.MatchString(d.version) {
			d.version = "unknown"
		}
	}
	if d.linux.Nftables == "" {
		d.linux.Nftables = "missing"
	}
	d.checks = []reportCheck{
		check("nftables", nftVersion != "", "FR_NFT_MISSING"),
		check("installed", installed, "FR_NOT_INSTALLED"),
		check("running", running, "FR_NOT_RUNNING"),
		sameVersion(installedVersion),
		warn("starts_by_itself", installed && !enabled, "FR_NOT_AT_BOOT"),
	}
	out, errOut, err := s.run("journalctl", "-u", "fragma", "-n", "200", "-o", "cat", "--no-pager")
	out = strings.TrimSpace(out)
	switch {
	case out != "" && out != "-- No entries --":
		d.logStatus, d.logLines = "read", strings.Split(out, "\n")
	case err != nil || strings.Contains(errOut, "permission"):
		d.logStatus = "not_readable" // sudo, or the systemd-journal group
	default:
		d.logStatus = "none"
	}
	return d
}
