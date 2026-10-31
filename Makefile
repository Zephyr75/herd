# Linux needs webkit2gtk-4.1 (Arch: pacman -S webkit2gtk-4.1) and the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
TAGS := $(if $(filter Linux,$(shell uname -s)),-tags webkit2_41,)

.PHONY: dev build test ui check

dev:           ## desktop app with hot reload
	wails dev $(TAGS)

build:         ## build/bin/herd
	wails build $(TAGS)

ui:            ## the UI alone in a browser, on the preview's mock data
	cd frontend && npm run dev

test:
	go test ./internal/...

check:
	go vet ./internal/... && cd frontend && npm run check
