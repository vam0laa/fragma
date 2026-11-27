.PHONY: build test e2e dist release
build:
	go build -trimpath -ldflags "-s -w" -o fragma .
test:
	go vet ./...
	go test ./...

# End-to-end test of the system-wide mode inside a private network namespace
# (no root needed, never touches your real network).
# The binary is built like a release (no cgo), and the sandbox gets its own
# resolv.conf that points at the fake ISP resolver.
e2e:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o fragma .
	go test -tags e2e -c -o e2e.test .
	echo "nameserver 203.0.113.53" > e2e.resolv.conf
	FRAGMA_BIN=$(CURDIR)/fragma unshare -Urnm sh -c '\
		mount --bind e2e.resolv.conf /etc/resolv.conf && \
		ip link set lo up && \
		ip addr add 203.0.113.5/32 dev lo && ip addr add 203.0.113.53/32 dev lo && ip addr add 10.1.2.3/32 dev lo && ip addr add 203.0.113.7/32 dev lo && ip addr add 203.0.113.8/32 dev lo && ip addr add 203.0.113.9/32 dev lo && \
		./e2e.test -test.run TestSandbox -test.v -test.count=1'
	@rm -f e2e.test e2e.resolv.conf

# The release files (Linux amd64 + arm64, Windows) with manifest and checksums,
# checked. Releases are made by .github/workflows/release.yml, see docs/releasing.md.
dist:
	scripts/release/dist.sh
	scripts/release/check.sh dist dist

release: test dist
