package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	binDest  = "/usr/local/bin/fragma"
	unitDest = "/etc/systemd/system/fragma.service"
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
		"should open now.",
		"",
		paint(cBold, "sudo fragma stop") + "        turn it off",
		paint(cBold, "sudo fragma start") + "       turn it on again",
		paint(cBold, "fragma status") + "           is it on?",
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
