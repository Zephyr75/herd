package settings

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch reloads the settings whenever a file under Dir changes (edited by
// hand or by herd) and calls onChange after each successful reload.
func (s *Store) Watch(ctx context.Context, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, d := range []string{s.Dir, filepath.Join(s.Dir, "context")} {
		if err := w.Add(d); err != nil {
			w.Close()
			return err
		}
	}
	go func() {
		defer w.Close()
		var timer *time.Timer
		fire := make(chan struct{}, 1)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				name := filepath.Base(ev.Name)
				if strings.HasSuffix(name, ".tmp") || !(strings.HasSuffix(name, ".toml") || strings.HasSuffix(name, ".md")) {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(200*time.Millisecond, func() {
					select {
					case fire <- struct{}{}:
					default:
					}
				})
			case <-fire:
				if err := s.Reload(); err != nil {
					log.Printf("settings: reload: %v", err)
					continue
				}
				onChange()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("settings: watch: %v", err)
			}
		}
	}()
	return nil
}
