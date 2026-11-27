package main

import (
	"errors"
	"net"
	"testing"
)

func TestAppRelayListensOnlyWhileAppsAreOn(t *testing.T) {
	var opened []net.Listener
	r := &appRelay{
		addrs: []string{"127.0.0.1:0", "127.0.0.1:0"},
		listen: func(network, addr string) (net.Listener, error) {
			ln, err := net.Listen(network, addr)
			if err == nil {
				opened = append(opened, ln)
			}
			return ln, err
		},
		serve: func(net.Listener) {},
	}
	closed := func(ln net.Listener) bool {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			c.Close()
		}
		return err != nil
	}
	if r.update(false); len(opened) != 0 || r.active() {
		t.Fatal("nothing may listen while apps are off")
	}
	r.update(true)
	r.update(true)
	if len(opened) != 2 || !r.active() || closed(opened[0]) {
		t.Fatal("two listeners, opened once", len(opened))
	}
	r.update(false)
	if r.active() || !closed(opened[0]) || !closed(opened[1]) {
		t.Fatal("switching apps off must close them")
	}
	r.update(true)
	if len(opened) != 4 {
		t.Fatal("and on again opens them again", len(opened))
	}
	r.update(false)
}

func TestAppRelayReportsAPortInUseOnce(t *testing.T) {
	reports, tries := 0, 0
	r := &appRelay{
		addrs:  []string{"127.0.0.1:443"},
		listen: func(string, string) (net.Listener, error) { tries++; return nil, errors.New("in use") },
		serve:  func(net.Listener) {},
		report: func(error) { reports++ },
	}
	for i := 0; i < 5; i++ {
		r.update(true)
	}
	if tries != 5 || reports != 1 {
		t.Fatalf("it must keep trying and say so once: %d tries, %d reports", tries, reports)
	}
}
