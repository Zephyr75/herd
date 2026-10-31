// Package artifacts parses artifact front matter and watches the run
// directories for writes.
package artifacts

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Field is one front matter key and value, in file order.
type Field struct {
	K string `json:"k"`
	V string `json:"v"`
}

// Artifact is a parsed markdown file.
type Artifact struct {
	Path   string  `json:"path"`
	File   string  `json:"file"`
	Exists bool    `json:"exists"`
	Front  []Field `json:"front"`
	Body   string  `json:"body"`
}

// Get returns a front matter value.
func (a Artifact) Get(k string) string {
	for _, f := range a.Front {
		if f.K == k {
			return f.V
		}
	}
	return ""
}

// Parse splits YAML front matter (flat key: value pairs) from the body.
func Parse(src string) ([]Field, string) {
	s := strings.ReplaceAll(strings.TrimPrefix(src, "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, src
	}
	rest := s[4:]
	idx := 0
	for {
		nl := strings.IndexByte(rest[idx:], '\n')
		line := rest[idx:]
		if nl >= 0 {
			line = rest[idx : idx+nl]
		}
		if strings.TrimSpace(line) == "---" {
			head, body := rest[:idx], ""
			if nl >= 0 {
				body = rest[idx+nl+1:]
			}
			return parseHead(head), strings.TrimLeft(body, "\n")
		}
		if nl < 0 {
			return nil, src // no closing ---
		}
		idx += nl + 1
	}
}

func parseHead(head string) []Field {
	var fields []Field
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		fields = append(fields, Field{K: strings.TrimSpace(k), V: strings.Trim(v, `"'`)})
	}
	return fields
}

// Read loads and parses an artifact; a missing file is not an error.
func Read(path string) (Artifact, error) {
	a := Artifact{Path: path, File: filepath.Base(path)}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	a.Exists = true
	a.Front, a.Body = Parse(string(b))
	return a, nil
}

// IsDone reports whether the artifact satisfies a done rule: key has one of
// values.
func (a Artifact) IsDone(key string, values []string) bool {
	if !a.Exists {
		return false
	}
	v := strings.ToLower(a.Get(key))
	for _, want := range values {
		if v == strings.ToLower(want) {
			return true
		}
	}
	return false
}

// Watch calls onWrite(path) for each markdown file written under root,
// including run directories created later.
func Watch(ctx context.Context, root string, onWrite func(path string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	add := func(dir string) {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = w.Add(p)
			}
			return nil
		})
	}
	add(root)
	go func() {
		defer w.Close()
		pending := map[string]*time.Timer{}
		fire := make(chan string, 64)
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-fire:
				delete(pending, p)
				onWrite(p)
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						add(ev.Name)
						continue
					}
				}
				if !strings.HasSuffix(ev.Name, ".md") || ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				p := ev.Name
				if t, ok := pending[p]; ok {
					t.Stop()
				}
				pending[p] = time.AfterFunc(150*time.Millisecond, func() {
					select {
					case fire <- p:
					case <-ctx.Done():
					}
				})
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("artifacts: watch: %v", err)
			}
		}
	}()
	return nil
}
