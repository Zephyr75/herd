package main

import (
	"context"
	"log"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"herd/internal/app"
)

// Shell connects the bound App service to the Wails runtime: events, the
// folder picker, and the app lifecycle.
type Shell struct {
	ctx context.Context
	svc *app.App
}

func (s *Shell) startup(ctx context.Context) {
	s.ctx = ctx
	if err := app.Start(s.svc, ctx, s); err != nil {
		log.Printf("herd: startup: %v", err)
		wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
			Type: wruntime.ErrorDialog, Title: "herd could not start", Message: err.Error(),
		})
		wruntime.Quit(ctx)
	}
}

func (s *Shell) shutdown(context.Context) { app.Stop(s.svc) }

// Emit sends an event to the frontend.
func (s *Shell) Emit(event string, data any) {
	if s.ctx != nil {
		wruntime.EventsEmit(s.ctx, event, data)
	}
}

func (s *Shell) emit(event string, data any) { s.Emit(event, data) }

// PickFolder opens the native folder picker.
func (s *Shell) PickFolder(title string) (string, error) {
	return wruntime.OpenDirectoryDialog(s.ctx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: false})
}
