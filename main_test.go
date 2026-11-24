package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := map[string]error{
		"ok":           nil,
		"reset":        errors.New("read tcp: connection reset by peer"),
		"closed":       errors.New("EOF"),
		"timeout":      context.DeadlineExceeded,
		"server-alert": errors.New("remote error: tls: unrecognized name"),
		"error":        errors.New("something else"),
	}
	for want, err := range cases {
		if got := classify(err); got != want {
			t.Errorf("classify(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestVerdict(t *testing.T) {
	ok := res{ok: 3, n: 3}
	dead := func(f string) res { return res{n: 3, fail: f} }
	if v := verdict(false, res{}, res{}); v != "IP/port unreachable" {
		t.Error(v)
	}
	if v := verdict(true, ok, ok); v != "reachable" {
		t.Error(v)
	}
	if v := verdict(true, res{ok: 1, n: 3, fail: "reset"}, ok); v != "FLAKY (1/3 ok)" {
		t.Error("intermittent results must not look like a clean block:", v)
	}
	if v := verdict(true, dead("reset"), ok); v != "SNI-BASED BLOCK (reset)" {
		t.Error(v)
	}
	if v := verdict(true, dead("timeout"), dead("timeout")); v != "blocked or down (timeout)" {
		t.Error(v)
	}
}

func TestResString(t *testing.T) {
	if (res{ok: 3, n: 3}).String() != "ok" || (res{n: 3, fail: "reset"}).String() != "reset" || (res{ok: 2, n: 3}).String() != "flaky 2/3" {
		t.Error("res.String")
	}
}

func TestDNSNote(t *testing.T) {
	ip := net.ParseIP
	if dnsNote(nil, nil) != "plain DNS failed" {
		t.Error("empty plain")
	}
	if dnsNote([]net.IP{ip("10.0.0.1")}, []net.IP{ip("1.2.3.4")}) != "POISONED" {
		t.Error("bogon must be POISONED")
	}
	if dnsNote([]net.IP{ip("1.2.3.4")}, []net.IP{ip("1.2.3.4"), ip("5.6.7.8")}) != "ok" {
		t.Error("overlap must be ok")
	}
	if dnsNote([]net.IP{ip("9.9.9.9")}, []net.IP{ip("1.2.3.4")}) != "differs" {
		t.Error("no overlap, no bogon must be differs")
	}
}

func TestFragmentRecord(t *testing.T) {
	body := []byte("0123456789abcdef")
	p := append([]byte{0x16, 0x03, 0x01, 0, byte(len(body))}, body...)
	r1, r2 := fragmentRecord(p, 6)
	if r1 == nil {
		t.Fatal("expected two records")
	}
	for _, r := range [][]byte{r1, r2} {
		if r[0] != 0x16 || r[1] != 0x03 || r[2] != 0x01 || int(r[3])<<8|int(r[4]) != len(r)-5 {
			t.Fatalf("bad record header: % x", r[:5])
		}
	}
	if string(append(append([]byte{}, r1[5:]...), r2[5:]...)) != string(body) {
		t.Fatal("record bodies must rejoin to the original handshake bytes")
	}
	if a, _ := fragmentRecord(p, 0); a != nil {
		t.Error("k=0 must not fragment")
	}
	if a, _ := fragmentRecord([]byte{0x17, 3, 3, 0, 1, 9}, 1); a != nil {
		t.Error("non-handshake record must not fragment")
	}
}

func TestPositions(t *testing.T) {
	p := []byte("....discord.com....")
	if posSNIStart(p, "discord.com") != 4 || posMidSNI(p, "discord.com") != 9 || posSNIStart(p, "nope.io") != -1 {
		t.Error("SNI positions wrong")
	}
}
