# Linux needs webkit2gtk-4.1 (Arch: pacman -S webkit2gtk-4.1) and the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
TAGS := $(if $(filter Linux,$(shell uname -s)),-tags webkit2_41,)
# go install puts wails in $(go env GOPATH)/bin, which is often not on PATH.
WAILS := $(or $(shell command -v wails 2>/dev/null),$(shell go env GOPATH)/bin/wails)

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
