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

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", paint(cRed, "✖"), msg)
	os.Exit(1)
}

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
	bin := filepath.Join(*prefix, binDest)
	if exe != bin {
		if err := copyFile(exe, bin, 0o755); err != nil {
			fail(T("fail.copy", err))
		}
	}
	unit := filepath.Join(*prefix, unitDest)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(unit, []byte(unitText), 0o644); err != nil {
		fail(T("fail.unit", err))
	}
	stepOK(T("step.installed"), binDest)
	if hf := filepath.Join(*prefix, defaultHostsFile); !exists(hf) {
		os.MkdirAll(filepath.Dir(hf), 0o755)
		os.WriteFile(hf, []byte(hostsTpl), 0o644)
	}
	if *noSys {
		return
	}
	if err := systemctl("daemon-reload"); err != nil {
		fail(T("fail.reload"))
	}
	quiet := func(args ...string) error {
		c := exec.Command("systemctl", args...)
		c.Stderr = os.Stderr
		return c.Run()
	}
	if err := quiet("enable", "fragma"); err != nil {
		fail(T("fail.enable"))
	}
	stepOK(T("step.service"), T("d.enabled"))
	if err := quiet("restart", "fragma"); err != nil { // restart: an upgrade must run the new binary
		fail(T("fail.start"))
	}
	time.Sleep(time.Second)
	if !active() {
		fail(T("fail.stayup"))
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
	os.Remove(filepath.Join(*prefix, binDest))
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

// runAdd appends host names to the user's list.
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
		fail(err.Error())
	}
	defer f.Close()
	for _, a := range fs.Args() {
		h := normalizeHost(a)
		switch {
		case h == "" || strings.ContainsAny(h, " /:"):
			fail(T("fail.badhost", a))
		case have[h]:
			stepOK(T("step.known"), h)
		default:
			fmt.Fprintln(f, h)
			stepOK(T("step.added"), h+" ("+T("d.applies")+")")
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
			fmt.Println("    " + paint(cDim, T("list.none")))
		}
		for _, x := range xs {
			fmt.Println("    " + x)
		}
		fmt.Println()
	}
	fmt.Println()
	show(T("list.builtin"), b, T("list.always"))
	show(T("list.yours"), u, defaultHostsFile)
	show(T("list.learned"), l, T("list.found"))
	fmt.Println("  " + paint(cDim, T("list.footer")))
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
