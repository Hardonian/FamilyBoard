.PHONY: all fmt vet lint test test-race build build-arm64 pi-bootstrap-check release-check clean

VERSION ?= 1.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "unknown")
LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(DATE)

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

build:
	go build -ldflags "$(LDFLAGS)" -o familyboard ./cmd/familyboard
	go build -ldflags "$(LDFLAGS)" -o familyboardctl ./cmd/familyboardctl

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboard-linux-arm64 ./cmd/familyboard
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o familyboardctl-linux-arm64 ./cmd/familyboardctl

pi-bootstrap-check:
	@echo "Checking deploy/pi shell scripts syntax..."
	@bash -n deploy/pi/bootstrap.sh
	@bash -n deploy/pi/install.sh
	@bash -n deploy/pi/update.sh
	@bash -n deploy/pi/verify.sh
	@bash -n deploy/pi/uninstall.sh
	@bash -n deploy/pi/create-migration-bundle.sh
	@bash -n deploy/pi/restore-migration-bundle.sh
	@bash -n deploy/pi/kiosk/familyboard-kiosk.sh
	@echo "All shell scripts passed syntax checks."

release-check: fmt vet test test-race build build-arm64 pi-bootstrap-check
	@echo "=== FamilyBoard Canonical Release Gate PASSED ==="

clean:
	rm -f familyboard familyboardctl familyboard.exe familyboardctl.exe familyboard-linux-arm64 familyboardctl-linux-arm64
	rm -rf backups/ diagnostics/ staging/
