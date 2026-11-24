package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", paint(cRed, "✖"), fmt.Sprintf(format, a...))
	os.Exit(1)
}

func needRoot(cmd string) {
	if os.Geteuid() != 0 {
		fail("this needs root: sudo fragma %s", cmd)
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
		fail("%v", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		fail("%v", err)
	}
	bin := filepath.Join(*prefix, binDest)
	if exe != bin {
		if err := copyFile(exe, bin, 0o755); err != nil {
			fail("installing the binary: %v", err)
		}
	}
	unit := filepath.Join(*prefix, unitDest)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		fail("%v", err)
	}
	if err := os.WriteFile(unit, []byte(unitText), 0o644); err != nil {
		fail("writing the service file: %v", err)
	}
	stepOK("Installed", binDest)
	if hf := filepath.Join(*prefix, defaultHostsFile); !exists(hf) {
		os.MkdirAll(filepath.Dir(hf), 0o755)
		os.WriteFile(hf, []byte(hostsTpl), 0o644)
	}
	if *noSys {
		return
	}
	if err := systemctl("daemon-reload"); err != nil {
		fail("systemctl daemon-reload failed (is this a systemd system?)")
	}
	quiet := func(args ...string) error {
		c := exec.Command("systemctl", args...)
		c.Stderr = os.Stderr
		return c.Run()
	}
	if err := quiet("enable", "fragma"); err != nil {
		fail("could not enable the service, see: journalctl -u fragma")
	}
	stepOK("Service", "enabled, starts at every boot")
	if err := quiet("restart", "fragma"); err != nil { // restart: an upgrade must run the new binary
		fail("could not start the service, see: journalctl -u fragma")
	}
	time.Sleep(time.Second)
	if !active() {
		fail("the service did not stay up, see: journalctl -u fragma -n 30")
	}
	stepOK("fragma", "running")
	fmt.Println()
	fmt.Print(box("fragma is ON", []string{
		"",
		"Discord, Roblox and other blocked sites",
		"should open now. Every other site is",
		"left exactly as it is.",
		"",
		paint(cBold, "sudo fragma stop") + "        turn it off",
		paint(cBold, "sudo fragma start") + "       turn it on again",
		paint(cBold, "fragma status") + "           is it on?",
		paint(cBold, "sudo fragma add <site>") + "  add a blocked site",
		paint(cBold, "sudo fragma uninstall") + "   remove everything",
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
	os.Remove(filepath.Join(*prefix, binDest))
	if !*noSys {
		systemctl("daemon-reload")
		removeRules() // in case the service was already gone
	}
	stepOK("Removed", "service, rules and program are gone")
}

// runControl wraps systemctl start|stop|restart.
func runControl(action string) {
	needRoot(action)
	if err := systemctl(action, "fragma"); err != nil {
		fail("systemctl %s fragma failed (installed? run the install command first)", action)
	}
	switch action {
	case "stop":
		fmt.Printf("  %s fragma is %s. Nothing is redirected anymore.\n", paint(cAmber, "○"), paint(cBold+";"+cAmber, "OFF"))
	default:
		time.Sleep(time.Second)
		if active() {
			fmt.Printf("  %s fragma is %s.\n", paint(cGreen, "●"), paint(cBold+";"+cGreen, "ON"))
		} else {
			fail("the service did not stay up, see: journalctl -u fragma -n 30")
		}
	}
}

func runStatus() {
	if active() {
		fmt.Printf("  %s fragma is %s  %s\n", paint(cGreen, "●"), paint(cBold+";"+cGreen, "ON"), paint(cDim, "(stop with: sudo fragma stop)"))
		return
	}
	fmt.Printf("  %s fragma is %s  %s\n", paint(cAmber, "○"), paint(cBold+";"+cAmber, "OFF"), paint(cDim, "(start with: sudo fragma start)"))
	os.Exit(3) // like systemctl is-active
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// runAdd appends host names to the user's list.
func runAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	prefix := fs.String("prefix", "", "root directory (testing)")
	fs.Parse(args)
	if fs.NArg() == 0 {
		fail("usage: sudo fragma add example.com [more.org ...]")
	}
	if *prefix == "" {
		needRoot("add")
	}
	path := filepath.Join(*prefix, defaultHostsFile)
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
		fail("%v", err)
	}
	defer f.Close()
	for _, a := range fs.Args() {
		h := normalizeHost(a)
		switch {
		case h == "" || strings.ContainsAny(h, " /:"):
			fail("not a host name: %q", a)
		case have[h]:
			stepOK("Known", h)
		default:
			fmt.Fprintln(f, h)
			stepOK("Added", h+" (applies within 10 seconds)")
		}
	}
}

// runList shows which hosts fragma changes.
func runList() {
	h := newHostlist(defaultHostsFile, filepath.Join(defaultStateDir, "learned.txt"))
	b, u, l := h.lists()
	show := func(title string, xs []string, hint string) {
		fmt.Printf("  %s  %s\n", paint(cBold, title), paint(cDim, hint))
		if len(xs) == 0 {
			fmt.Println("    " + paint(cDim, "(none)"))
		}
		for _, x := range xs {
			fmt.Println("    " + x)
		}
		fmt.Println()
	}
	fmt.Println()
	show("Built in", b, "always")
	show("Yours", u, defaultHostsFile)
	show("Learned", l, "found blocked by fragma itself")
	fmt.Println("  " + paint(cDim, "Every other site is passed through unchanged."))
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
			fmt.Printf("  %s DNS-over-HTTPS is %s\n", paint(cGreen, "●"), paint(cBold+";"+cGreen, "ON"))
		} else {
			fmt.Printf("  %s DNS is %s (your system DNS is used as is)\n", paint(cDim, "○"), paint(cBold, "untouched"))
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
			fail("%v", err)
		}
	case "off":
		if *prefix == "" {
			needRoot("dns off")
		}
		os.Remove(path)
		os.Remove(filepath.Dir(path))
	default:
		fail("usage: sudo fragma dns on [--doh URL] | off | status")
	}
	if !*noSys {
		systemctl("daemon-reload")
		if err := systemctl("restart", "fragma"); err != nil {
			fail("could not restart the service, see: journalctl -u fragma")
		}
	}
	if action == "on" {
		stepOK("DNS", "answered over DNS-over-HTTPS; your lookups go to that provider, not to your ISP")
	} else {
		stepOK("DNS", "untouched again")
	}
}
