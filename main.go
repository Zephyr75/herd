// Command herd is a desktop app that runs Claude Code through a pipeline of
// agents, on tasks from several repositories, each in its own git worktree.
package main

import (
	"embed"
	"io/fs"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"herd/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:plugin
var pluginFiles embed.FS

func main() {
	plugin, err := fs.Sub(pluginFiles, "plugin")
	if err != nil {
		log.Fatal(err)
	}
	svc := app.New(app.DefaultHome(), plugin)
	shell := &Shell{svc: svc}

	opts := &options.App{
		Title:            "herd",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 0x18, G: 0x19, B: 0x1b, A: 0xff},
		// herd's top bar is the titlebar: it drags the window and holds the
		// window controls. macOS keeps its traffic lights (TitleBarHiddenInset).
		Frameless:   runtime.GOOS != "darwin",
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   shell.startup,
		OnShutdown:  shell.shutdown,
		Bind:        []interface{}{svc},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			About:                &mac.AboutInfo{Title: "herd", Message: "Claude Code agents, one worktree per task."},
		},
		Linux: &linux.Options{ProgramName: "herd"},
	}
	// ⌘P and ⌘N also work as menu items on macOS; elsewhere the frontend
	// handles every shortcut and the window has no menu bar.
	if runtime.GOOS == "darwin" {
		opts.Menu = appMenu(shell)
	}
	if opts.Frameless {
		useClientSideTitlebar()
	}
	if err := wails.Run(opts); err != nil {
		log.Fatal(err)
	}
}

func appMenu(s *Shell) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	tasks := m.AddSubmenu("Tasks")
	tasks.AddText("Go to task…", keys.CmdOrCtrl("p"), func(*menu.CallbackData) { s.emit("menu", "palette") })
	tasks.AddText("New task…", keys.CmdOrCtrl("n"), func(*menu.CallbackData) { s.emit("menu", "newTask") })
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}
