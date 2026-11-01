# Linux needs webkit2gtk-4.1 (Arch: pacman -S webkit2gtk-4.1) and the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
TAGS := $(if $(filter Linux,$(shell uname -s)),-tags webkit2_41,)
# Prefer the v2 CLI from go install: a `wails` on PATH may be v3 (e.g. Arch's package), which lacks -tags.
GOBIN_WAILS := $(shell go env GOPATH)/bin/wails
WAILS := $(or $(wildcard $(GOBIN_WAILS)),$(shell command -v wails 2>/dev/null),$(GOBIN_WAILS))

.PHONY: dev build test ui check

dev:           ## desktop app with hot reload
	$(WAILS) dev $(TAGS)

build:         ## build/bin/herd
	$(WAILS) build $(TAGS)

ui:            ## the UI alone in a browser, on the preview's mock data
	cd frontend && npm run dev

test:
	go test ./internal/...

check:
	go vet ./internal/... && cd frontend && npm run check
