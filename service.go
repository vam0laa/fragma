//go:build linux

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	binDest  = "/usr/local/bin/fragma"
	unitDest = "/etc/systemd/system/fragma.service"
	dropIn   = "/etc/systemd/system/fragma.service.d/dns.conf"
	hostsTpl = `# Extra sites that are blocked for you, one per line (subdomains are included).
# fragma already knows Discord and Roblox and learns blocked sites by itself.
# Every other site is left completely alone.
# Changes apply within 10 seconds, no restart needed.
#
# example.com
`
	unitText = `[Unit]
Description=fragma: get around SNI-based access blocks (TLS ClientHello fragmentation)
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/fragma run
# Whatever happens to the daemon, never leave the redirect rules behind.
ExecStopPost=-/usr/bin/env nft delete table ip fragma
Restart=on-failure
RestartSec=2
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
StateDirectory=fragma

[Install]
WantedBy=multi-user.target
`
)

func needRoot(cmd string) {
	if os.Geteuid() != 0 {
		fail(T("fail.root", cmd))
	}
}

func systemctl(args ...string) error {
	c := exec.Command("systemctl", args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func active() bool { return exec.Command("systemctl", "is-active", "--quiet", "fragma").Run() == nil }

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst) // atomic, also works while the old binary is running
}

// runInstall copies this binary to /usr/local/bin, installs the systemd unit
// and starts it. --prefix and --no-systemctl exist for tests and packaging.
func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	prefix := fs.String("prefix", "", "install under this directory instead of / (testing, packaging)")
	noSys := fs.Bool("no-systemctl", false, "only write the files")
	fs.Parse(args)
	if *prefix == "" {
		needRoot("install")
	}
	exe, err := os.Executable()
	if err != nil {
		fail(err.Error())
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		fail(err.Error())
	}
	// try this program on this system before anything is changed
	if *prefix == "" {
		if err := checkRules(ruleset(defaultTCPPort, defaultDNSPort, false)); err != nil {
			fail(T("fail.precheck", err))
		}
	}
	bin, unit := filepath.Join(*prefix, binDest), filepath.Join(*prefix, unitDest)
	service := !*noSys
	// an interrupted update would leave things half done
	signal.Ignore(os.Interrupt, syscall.SIGHUP)
	was, err := snapshot(bin, unit, service)
	if err != nil {
		fail(T("fail.copy", err))
	}
	// With a prefix there are no real rules to look for.
	ready := func() bool { return *prefix != "" || nft("", "list", "table", "ip", nftTable) == nil }
	err = func() error {
		if exe != bin {
			if err := copyFile(exe, bin, 0o755); err != nil {
				return errors.New(T("fail.copy", err))
			}
		}
		if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(unit, []byte(unitText), 0o644); err != nil {
			return errors.New(T("fail.unit", err))
		}
		stepOK(T("step.installed"), binDest)
		if hf := filepath.Join(*prefix, defaultHostsFile); !exists(hf) {
			os.MkdirAll(filepath.Dir(hf), 0o755)
			os.WriteFile(hf, []byte(hostsTpl), 0o644)
		}
		if !service {
			return nil
		}
		return activate(ready)
	}()
	if err != nil {
		fail(T("upd.failed", err) + " " + rollBack(was, service, ready))
	}
	if err := keepPrevious(was); err != nil {
		fmt.Fprintln(os.Stderr, "  ! "+err.Error())
	}
	if !service {
		return
	}
	stepOK(T("step.running"), T("d.running"))
	fmt.Println()
	cmd := func(c, desc string) string { return padRight(paint(cBold, c), 24) + desc }
	fmt.Print(box(T("box.title"), []string{
		"",
		T("box.l1"),
		T("box.l2"),
		T("box.l3"),
		"",
		cmd("sudo fragma stop", T("box.stop")),
		cmd("sudo fragma start", T("box.start")),
		cmd("fragma status", T("box.status")),
		cmd("sudo fragma add <site>", T("box.add")),
		cmd("fragma report", T("box.report")),
		cmd("sudo fragma uninstall", T("box.uninstall")),
		"",
	}, cGreen))
	fmt.Println()
}

func runUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	prefix := fs.String("prefix", "", "uninstall from this directory instead of /")
	noSys := fs.Bool("no-systemctl", false, "only remove the files")
	fs.Parse(args)
	if *prefix == "" {
		needRoot("uninstall")
	}
	if !*noSys {
		systemctl("disable", "--now", "fragma")
	}
	os.Remove(filepath.Join(*prefix, unitDest))
	for _, suffix := range []string{"", keptSuffix, rollbackSuffix} {
		os.Remove(filepath.Join(*prefix, binDest) + suffix)
	}
	if !*noSys {
		systemctl("daemon-reload")
		removeRules() // in case the service was already gone
	}
	stepOK(T("step.removed"), T("d.removed"))
}

// runControl wraps systemctl start|stop|restart.
func runControl(action string) {
	needRoot(action)
	if err := systemctl(action, "fragma"); err != nil {
		fail(T("fail.ctl", action))
	}
	switch action {
	case "stop":
		fmt.Printf("  %s %s\n", paint(cAmber, "○"), T("ctl.off", paint(cBold+";"+cAmber, T("word.off"))))
	default:
		time.Sleep(time.Second)
		if active() {
			fmt.Printf("  %s %s\n", paint(cGreen, "●"), T("ctl.on", paint(cBold+";"+cGreen, T("word.on"))))
		} else {
			fail(T("fail.stayup"))
		}
	}
}

func runStatus() {
	if active() {
		fmt.Printf("  %s %s\n", paint(cGreen, "●"), T("st.on", paint(cBold+";"+cGreen, T("word.on")), paint(cDim, T("st.onhint"))))
		return
	}
	fmt.Printf("  %s %s\n", paint(cAmber, "○"), T("st.off", paint(cBold+";"+cAmber, T("word.off")), paint(cDim, T("st.offhint"))))
	os.Exit(3) // like systemctl is-active
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func platformCommand(string, []string) bool { return false }

// runAdd appends host names to the system-wide list.
func runAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	prefix := fs.String("prefix", "", "root directory (testing)")
	fs.Parse(args)
	if fs.NArg() == 0 {
		fail(T("fail.addusage"))
	}
	if *prefix == "" {
		needRoot("add")
	}
	appendHosts(filepath.Join(*prefix, defaultHostsFile), fs.Args())
}

// runList shows which hosts fragma changes.
func runList() {
	printLists(newHostlist(defaultHostsFile, filepath.Join(defaultStateDir, "learned.txt")), defaultHostsFile)
}

// runDNS switches the optional DNS-over-HTTPS part on or off with a systemd drop-in.
func runDNS(args []string) {
	fs := flag.NewFlagSet("dns", flag.ExitOnError)
	prefix := fs.String("prefix", "", "root directory (testing)")
	noSys := fs.Bool("no-systemctl", false, "only write the files")
	doh := fs.String("doh", "", "your own DNS-over-HTTPS URL(s), comma separated (default: Cloudflare)")
	// flags and the action word in any order: `dns on --doh URL` or `dns --doh URL on`
	action := ""
	for {
		fs.Parse(args)
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		if action == "" {
			action = rest[0]
		}
		args = rest[1:]
	}
	path := filepath.Join(*prefix, dropIn)
	switch action {
	case "status":
		if exists(path) {
			fmt.Printf("  %s %s\n", paint(cGreen, "●"), T("dns.on", paint(cBold+";"+cGreen, T("word.on"))))
		} else {
			fmt.Printf("  %s %s\n", paint(cDim, "○"), T("dns.off", paint(cBold, T("dns.untouched"))))
		}
		return
	case "on":
		if *prefix == "" {
			needRoot("dns on")
		}
		exec := "/usr/local/bin/fragma run --dns"
		if *doh != "" {
			exec += " --doh " + *doh
		}
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte("[Service]\nExecStart=\nExecStart="+exec+"\n"), 0o644); err != nil {
			fail(err.Error())
		}
	case "off":
		if *prefix == "" {
			needRoot("dns off")
		}
		os.Remove(path)
		os.Remove(filepath.Dir(path))
	default:
		fail(T("fail.dnsusage"))
	}
	if !*noSys {
		systemctl("daemon-reload")
		if err := systemctl("restart", "fragma"); err != nil {
			fail(T("fail.restart"))
		}
	}
	if action == "on" {
		stepOK(T("step.dns"), T("d.dnson"))
	} else {
		stepOK(T("step.dns"), T("d.dnsoff"))
	}
}
