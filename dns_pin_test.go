package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestPinnedDialer(t *testing.T) {
	lookups, answer := 0, []net.IP{net.ParseIP("192.0.2.1")}
	var dialled []string
	dead := map[string]bool{}
	p := &pinnedDialer{
		dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialled = append(dialled, addr)
			if dead[addr] {
				return nil, errors.New("down")
			}
			c, _ := net.Pipe()
			return c, nil
		},
		lookup: func(context.Context, string) ([]net.IP, error) { lookups++; return answer, nil },
	}
	ctx := context.Background()
	if err := p.pin(ctx, []string{"https://1.1.1.1/dns-query", "https://doh.test/dns-query"}); err != nil || lookups != 1 {
		t.Fatal("only the server given by name is looked up", err, lookups)
	}
	for i := 0; i < 3; i++ {
		if _, err := p.DialContext(ctx, "tcp", "doh.test:443"); err != nil {
			t.Fatal(err)
		}
	}
	if lookups != 1 || dialled[0] != "192.0.2.1:443" {
		t.Fatal("the remembered address must be used", lookups, dialled)
	}
	dead["192.0.2.1:443"], answer = true, []net.IP{net.ParseIP("192.0.2.2")}
	if _, err := p.DialContext(ctx, "tcp", "doh.test:443"); err != nil || lookups != 2 {
		t.Fatal("a dead address is looked up again", err, lookups)
	}
	if _, err := p.DialContext(ctx, "tcp", "1.1.1.1:443"); err != nil || lookups != 2 {
		t.Fatal("addresses are dialled as they are", err)
	}
	for _, bad := range []string{"", "not a url", "https:///dns-query"} {
		if p.pin(ctx, []string{bad}) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
