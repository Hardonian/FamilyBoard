ifeq ($(OS),Windows_NT)
    SHELL := C:/PROGRA~1/Git/bin/bash.exe
    .SHELLFLAGS := -c
    BASH_BIN := C:/PROGRA~1/Git/bin/bash.exe
else
    SHELL := /bin/bash
    .SHELLFLAGS := -c
    BASH_BIN := bash
endif

.PHONY: all fmt vet lint staticcheck vuln security-check tools-check verify-mod check-version check-js test test-race build build-arm64 package-arm64 package-all pi-bootstrap-check release-check release-package clean

VERSION ?= $(shell cat VERSION)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
LDFLAGS = -s -w -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Version=$(VERSION) -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Commit=$(COMMIT) -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Date=$(DATE)

all: build

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: vet

tools-check:
	@go version >/dev/null

staticcheck: tools-check
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

vuln: tools-check
	go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

security-check: staticcheck vuln

verify-mod:
	go mod verify

check-version:
	@"$(BASH_BIN)" scripts/check_version.sh

check-js:
	node --check web/static/js/kiosk.js
	node --check web/static/js/admin.js
	node --check web/static/sw.js

test:
	go test -v ./...

test-race:
	go test -race ./...

bench:
	go test -v -bench=. -benchmem ./tests/...

build:
	go build -ldflags "$(LDFLAGS)" -o familyboard ./cmd/familyboard
	go build -ldflags "$(LDFLAGS)" -o familyboardctl ./cmd/familyboardctl

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboard-linux-arm64 ./cmd/familyboard
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboardctl-linux-arm64 ./cmd/familyboardctl

build-radxa: build-arm64
	@echo "Native Radxa Dragon ARM64 binaries built successfully."

package-arm64: build-arm64
	@"$(BASH_BIN)" scripts/package_release.sh $(VERSION)

package-all:
	@"$(BASH_BIN)" scripts/package_portable.sh $(VERSION)

package-radxa: build-radxa
	@mkdir -p dist
	@tar -czf dist/familyboard-radxa-dragon-$(VERSION).tar.gz familyboard-linux-arm64 familyboardctl-linux-arm64 deploy/radxa/ config.example.toml
	@echo "Generated dist/familyboard-radxa-dragon-$(VERSION).tar.gz"

radxa-check:
	@echo "Checking deploy/radxa shell scripts syntax..."
	@"$(BASH_BIN)" -n deploy/radxa/familyboard-kiosk.sh
	@"$(BASH_BIN)" -n deploy/radxa/setup-radxa.sh
	@"$(BASH_BIN)" -n deploy/radxa/migrate-to-nvme.sh
	@"$(BASH_BIN)" -n deploy/radxa/run-standalone.sh
	@"$(BASH_BIN)" -n scripts/prepare_radxa_sd.sh
	@echo "All Radxa Dragon shell scripts passed syntax checks."

prepare-radxa-sd: build-arm64
	@"$(BASH_BIN)" scripts/prepare_radxa_sd.sh $(OUTPUT)

pi-bootstrap-check:
	@echo "Checking deploy/pi shell scripts syntax..."
	@"$(BASH_BIN)" -n deploy/pi/bootstrap.sh
	@"$(BASH_BIN)" -n deploy/pi/install.sh
	@"$(BASH_BIN)" -n deploy/pi/update.sh
	@"$(BASH_BIN)" -n deploy/pi/verify.sh
	@"$(BASH_BIN)" -n deploy/pi/uninstall.sh
	@"$(BASH_BIN)" -n deploy/pi/create-migration-bundle.sh
	@"$(BASH_BIN)" -n deploy/pi/restore-migration-bundle.sh
	@"$(BASH_BIN)" -n deploy/pi/kiosk/familyboard-kiosk.sh
	@"$(BASH_BIN)" -n deploy/pi/familyboard-setup.sh
	@"$(BASH_BIN)" -n familyboard-setup.sh
	@"$(BASH_BIN)" -n scripts/prepare_sd.sh
	@"$(BASH_BIN)" -n scripts/package_release.sh
	@"$(BASH_BIN)" -n scripts/package_portable.sh
	@"$(BASH_BIN)" -n scripts/check_version.sh
	@echo "All shell scripts passed syntax checks."

prepare-sd:
	@"$(BASH_BIN)" scripts/prepare_sd.sh $(SD)

release-check: check-version fmt vet security-check verify-mod check-js test test-race build build-arm64 radxa-check pi-bootstrap-check
	@echo "=== FamilyBoard Canonical Release Gate PASSED ==="

release-package: release-check package-all
	@echo "=== FamilyBoard $(VERSION) release artifacts are ready in dist/releases/$(VERSION) ==="

clean:
	rm -f familyboard familyboardctl familyboard.exe familyboardctl.exe familyboard-linux-arm64 familyboardctl-linux-arm64
	rm -rf backups/ diagnostics/ staging/ dist/
