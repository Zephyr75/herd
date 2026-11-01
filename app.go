package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"herd/internal/app"
)

// Shell connects the bound App service to the Wails runtime: events, the
// folder picker, and the app lifecycle.
type Shell struct {
	ctx      context.Context
	svc      *app.App
	notifyOK atomic.Bool
	notifyN  atomic.Int64
}

func (s *Shell) startup(ctx context.Context) {
	s.ctx = ctx
	if err := wruntime.InitializeNotifications(ctx); err != nil {
		log.Printf("herd: notifications: %v", err)
	} else {
		s.notifyOK.Store(true)
		// A click on a notification brings herd forward on that task.
		wruntime.OnNotificationResponse(ctx, func(r wruntime.NotificationResult) {
			if r.Error != nil {
				return
			}
			wruntime.WindowUnminimise(ctx)
			wruntime.WindowShow(ctx)
			if id, ok := r.Response.UserInfo["taskId"].(string); ok {
				s.emit("menu", "task:"+id)
			}
		})
	}
	if err := app.Start(s.svc, ctx, s); err != nil {
		log.Printf("herd: startup: %v", err)
		wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
			Type: wruntime.ErrorDialog, Title: "herd could not start", Message: err.Error(),
		})
		wruntime.Quit(ctx)
	}
}

func (s *Shell) shutdown(ctx context.Context) {
	if s.notifyOK.Load() {
		wruntime.CleanupNotifications(ctx)
	}
	app.Stop(s.svc)
}

// Notify shows a desktop notification.
func (s *Shell) Notify(title, body, taskID string) {
	if s.ctx == nil || !s.notifyOK.Load() {
		return
	}
	err := wruntime.SendNotification(s.ctx, wruntime.NotificationOptions{
		ID: fmt.Sprintf("herd-%d", s.notifyN.Add(1)), Title: title, Body: body,
		Data: map[string]interface{}{"taskId": taskID},
	})
	if err != nil {
		log.Printf("herd: notify: %v", err)
	}
}

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
