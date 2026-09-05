ifeq ($(OS),Windows_NT)
    SHELL := C:/PROGRA~1/Git/bin/bash.exe
    .SHELLFLAGS := -c
    BASH_BIN := C:/PROGRA~1/Git/bin/bash.exe
else
    SHELL := /bin/bash
    .SHELLFLAGS := -c
    BASH_BIN := bash
endif

.PHONY: all fmt vet lint test test-race build build-arm64 pi-bootstrap-check release-check clean

VERSION ?= 1.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(DATE)

all: build

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: vet

test:
	go test -v ./...

test-race:
	go test -race ./tests/...

bench:
	go test -v -bench=. -benchmem ./tests/...

build:
	go build -ldflags "$(LDFLAGS)" -o familyboard ./cmd/familyboard
	go build -ldflags "$(LDFLAGS)" -o familyboardctl ./cmd/familyboardctl

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboard-linux-arm64 ./cmd/familyboard
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboardctl-linux-arm64 ./cmd/familyboardctl

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
	@echo "All shell scripts passed syntax checks."

release-check: fmt vet test test-race build build-arm64 pi-bootstrap-check
	@echo "=== FamilyBoard Canonical Release Gate PASSED ==="

clean:
	rm -f familyboard familyboardctl familyboard.exe familyboardctl.exe familyboard-linux-arm64 familyboardctl-linux-arm64
	rm -rf backups/ diagnostics/ staging/
