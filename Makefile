.PHONY: build test e2e release
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

# Static Linux binaries for GitHub Releases (amd64 + arm64) with checksums.
release: test
	rm -rf dist && mkdir -p dist
	for arch in amd64 arm64; do \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w" -o dist/fragma . && \
		tar -C dist --owner=0 --group=0 --numeric-owner -czf dist/fragma-linux-$$arch.tar.gz fragma && rm dist/fragma; \
	done
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fragma.exe .
	bsdtar --format zip -cf dist/fragma-windows-amd64.zip -C dist fragma.exe && rm dist/fragma.exe
	cd dist && sha256sum *.tar.gz *.zip > SHA256SUMS
