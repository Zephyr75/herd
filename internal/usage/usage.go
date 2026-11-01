// Package usage totals the tokens of a Claude Code
// session from its transcript (~/.claude/projects/*/<session>.jsonl, plus
// <session>/subagents/*.jsonl). Files are read incrementally and cached.
package usage

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Usage is a token total.
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

// Add returns u + v.
func (u Usage) Add(v Usage) Usage {
	return Usage{u.Input + v.Input, u.Output + v.Output, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite}
}

// Tokens is every token the session processed.
func (u Usage) Tokens() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

type rawUsage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
}

type line struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string    `json:"id"`
		Usage *rawUsage `json:"usage"`
	} `json:"message"`
}

// file is one transcript read up to off. A message is logged once per
// content block with the same id; the last line wins.
type file struct {
	off  int64
	byID map[string]Usage
}

// Reader caches parsed transcripts.
type Reader struct {
	Dir   string // Claude Code's config dir (holds projects/)
	mu    sync.Mutex
	files map[string]*file
	paths map[string]string // session id -> transcript path
}

// NewReader reads transcripts under dir/projects.
func NewReader(dir string) *Reader {
	return &Reader{Dir: dir, files: map[string]*file{}, paths: map[string]string{}}
}

// Session returns a session's usage, including its subagents. A session
// without a transcript is zero.
func (r *Reader) Session(id string) Usage {
	if id == "" {
		return Usage{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	path, ok := r.paths[id]
	if !ok {
		m, _ := filepath.Glob(filepath.Join(r.Dir, "projects", "*", id+".jsonl"))
		if len(m) == 0 {
			return Usage{}
		}
		path = m[0]
		r.paths[id] = path
	}
	total := r.read(path)
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, s := range subs {
		total = total.Add(r.read(s))
	}
	return total
}

// read parses what was appended to path since the last call.
func (r *Reader) read(path string) Usage {
	f := r.files[path]
	if f == nil {
		f = &file{byID: map[string]Usage{}}
		r.files[path] = f
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() < f.off {
		*f = file{byID: map[string]Usage{}} // rewritten: start over
	}
	if fh, err := os.Open(path); err == nil {
		if _, err := fh.Seek(f.off, io.SeekStart); err == nil {
			br := bufio.NewReaderSize(fh, 256<<10)
			for {
				b, err := br.ReadBytes('\n')
				if err != nil {
					break // a partial last line is read again next time
				}
				f.off += int64(len(b))
				if !strings.Contains(string(b), `"usage"`) {
					continue
				}
				var l line
				if json.Unmarshal(b, &l) != nil || l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
					continue
				}
				r := l.Message.Usage
				f.byID[l.Message.ID] = Usage{Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite}
			}
		}
		fh.Close()
	}
	var total Usage
	for _, u := range f.byID {
		total = total.Add(u)
	}
	return total
}
