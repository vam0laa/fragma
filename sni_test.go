package main

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"
)

// realClientHello captures the first record that Go's TLS client writes.
func realClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	a, b := net.Pipe()
	go func() {
		c := tls.Client(a, &tls.Config{ServerName: serverName, InsecureSkipVerify: true})
		c.Handshake()
	}()
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	rec, err := readRecord(b)
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, b)
	return rec
}

func TestFindSNI(t *testing.T) {
	rec := realClientHello(t, "discord.com")
	host, off, ok := findSNI(rec)
	if !ok || host != "discord.com" {
		t.Fatalf("host=%q ok=%v", host, ok)
	}
	if string(rec[off:off+len(host)]) != "discord.com" {
		t.Fatal("offset does not point at the host name")
	}
}

func TestFindSNIMissing(t *testing.T) {
	if _, _, ok := findSNI(realClientHello(t, "")); ok {
		t.Error("a ClientHello without SNI must report ok=false")
	}
	if _, _, ok := findSNI([]byte{0x17, 3, 3, 0, 1, 0}); ok {
		t.Error("non-handshake data must report ok=false")
	}
	if _, _, ok := findSNI(nil); ok {
		t.Error("nil must report ok=false")
	}
	// truncated and garbage input must never panic
	rec := realClientHello(t, "discord.com")
	for i := 0; i < len(rec); i++ {
		findSNI(rec[:i])
	}
}

func TestFragmentClientHello(t *testing.T) {
	rec := realClientHello(t, "discord.com")
	out := fragmentClientHello(rec)
	if len(out) != len(rec)+5 {
		t.Fatalf("expected two records (%d extra bytes), got %d vs %d", 5, len(out), len(rec))
	}
	r1, r2 := out[:5+(int(out[3])<<8|int(out[4]))], out[5+(int(out[3])<<8|int(out[4])):]
	if r1[0] != 0x16 || r2[0] != 0x16 {
		t.Fatal("both pieces must be handshake records")
	}
	if string(append(append([]byte{}, r1[5:]...), r2[5:]...)) != string(rec[5:]) {
		t.Fatal("record bodies must rejoin to the original ClientHello")
	}
	host, _, _ := findSNI(rec)
	// the host name must no longer sit completely inside the first record
	if string(r1[5:]) != "" && containsAll(r1[5:], host) {
		t.Fatal("the whole SNI is still inside the first record")
	}
	if got := fragmentClientHello(realClientHello(t, "")); len(got) == len(out) {
		t.Error("no SNI: record must stay unchanged")
	}
}

func containsAll(b []byte, s string) bool { return len(s) > 0 && indexOf(b, s) >= 0 }
func indexOf(b []byte, s string) int {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}
