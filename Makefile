.PHONY: build test e2e release
build:
	go build -trimpath -ldflags "-s -w" -o fragma .
test:
	go vet ./...
	go test ./...

# End-to-end test of the system-wide mode inside a private network namespace
# (no root needed, never touches your real network).
e2e: build
	go test -tags e2e -c -o e2e.test .
	FRAGMA_BIN=$(CURDIR)/fragma unshare -Urn sh -c '\
		ip link set lo up && \
		ip addr add 203.0.113.5/32 dev lo && ip addr add 203.0.113.53/32 dev lo && ip addr add 10.1.2.3/32 dev lo && ip addr add 203.0.113.7/32 dev lo && ip addr add 203.0.113.8/32 dev lo && \
		./e2e.test -test.run TestSandbox -test.v -test.count=1'
	@rm -f e2e.test

# Static Linux binaries for GitHub Releases (amd64 + arm64) with checksums.
release: test
	rm -rf dist && mkdir -p dist
	for arch in amd64 arm64; do \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w" -o dist/fragma . && \
		tar -C dist --owner=0 --group=0 --numeric-owner -czf dist/fragma-linux-$$arch.tar.gz fragma && rm dist/fragma; \
	done
	cd dist && sha256sum *.tar.gz > SHA256SUMS
