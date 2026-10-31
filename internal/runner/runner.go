// Package runner owns one PTY per live step: start, resume, input, resize,
// scrollback replay for viewers, and reaping idle sessions.
package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Spec describes the process to start for a step.
type Spec struct {
	StepID string
	Dir    string
	Env    []string // added to os.Environ()
	Unset  []string // variables removed from os.Environ() before Env is added
	Bin    string
	Args   []string
}

// ExitFunc is called once a session's process has exited. killed is true
// when the runner ended it (reaping or Stop).
type ExitFunc func(stepID string, code int, killed bool)

const scrollbackLimit = 2 << 20 // 2 MiB per session

// Session is one live process in a PTY.
type Session struct {
	StepID string
	cmd    *exec.Cmd
	pty    *os.File

	mu         sync.Mutex
	scrollback []byte
	viewers    map[*Viewer]struct{}
	lastSeen   time.Time // last time a viewer detached or output arrived
	killed     bool
	exited     bool
	exitCode   int
	done       chan struct{}
}

// Viewer receives a session's output.
type Viewer struct {
	C       chan []byte
	s       *Session
	closeMu sync.Once
}

// Runner tracks every live session, and the last output of sessions that
// ended during this run so a viewer can still see why a process exited.
type Runner struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ended    map[string]*Session
	order    []string // ended, oldest first
	onExit   ExitFunc
}

const keepEnded = 32

// New creates a runner; onExit is called after each process ends.
func New(onExit ExitFunc) *Runner {
	return &Runner{sessions: map[string]*Session{}, ended: map[string]*Session{}, onExit: onExit}
}

// Ended returns the most recent session of a step that exited during this
// run; Attach on it replays its output and reports the exit.
func (r *Runner) Ended(stepID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.ended[stepID]
	return s, ok
}

func (r *Runner) remember(s *Session) {
	if _, ok := r.ended[s.StepID]; !ok {
		r.order = append(r.order, s.StepID)
	}
	r.ended[s.StepID] = s
	for len(r.order) > keepEnded {
		delete(r.ended, r.order[0])
		r.order = r.order[1:]
	}
}

func environ(unset []string) []string {
	drop := map[string]bool{}
	for _, k := range unset {
		drop[k] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return out
}

// ErrRunning is returned by Start when the step already has a live session.
var ErrRunning = errors.New("step already has a live session")

// Start launches spec in a new PTY.
func (r *Runner) Start(spec Spec) (*Session, error) {
	r.mu.Lock()
	if s, ok := r.sessions[spec.StepID]; ok && !s.Exited() {
		r.mu.Unlock()
		return s, ErrRunning
	}
	r.mu.Unlock()

	cmd := exec.Command(spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(environ(spec.Unset), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Env = append(cmd.Env, spec.Env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return nil, err
	}
	s := &Session{StepID: spec.StepID, cmd: cmd, pty: f, viewers: map[*Viewer]struct{}{}, lastSeen: time.Now(), done: make(chan struct{})}
	r.mu.Lock()
	r.sessions[spec.StepID] = s
	r.mu.Unlock()

	go s.readLoop()
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		s.mu.Lock()
		s.exited, s.exitCode = true, code
		killed := s.killed
		s.mu.Unlock()
		f.Close()
		<-s.done // wait for the reader to drain
		s.closeViewers()
		r.mu.Lock()
		if r.sessions[spec.StepID] == s {
			delete(r.sessions, spec.StepID)
		}
		r.remember(s)
		r.mu.Unlock()
		if r.onExit != nil {
			r.onExit(spec.StepID, code, killed)
		}
	}()
	return s, nil
}

// Get returns a step's live session.
func (r *Runner) Get(stepID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[stepID]
	if ok && s.Exited() {
		return nil, false
	}
	return s, ok
}

// Live returns the ids of every live session.
func (r *Runner) Live() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.sessions))
	for id, s := range r.sessions {
		if !s.Exited() {
			out = append(out, id)
		}
	}
	return out
}

// Stop ends a step's session.
func (r *Runner) Stop(stepID string) {
	if s, ok := r.Get(stepID); ok {
		s.Kill()
	}
}

// StopAll ends every session (on quit).
func (r *Runner) StopAll() {
	r.mu.Lock()
	all := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		all = append(all, s)
	}
	r.mu.Unlock()
	for _, s := range all {
		s.Kill()
	}
	for _, s := range all {
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
		}
	}
}

// Reap ends sessions that canReap approves, have no viewer and have been
// quiet for at least grace. It also ends the oldest reapable sessions while
// more than max are live.
func (r *Runner) Reap(grace time.Duration, max int, canReap func(stepID string) bool) {
	r.mu.Lock()
	type cand struct {
		s    *Session
		seen time.Time
	}
	var cands []cand
	live := 0
	for id, s := range r.sessions {
		if s.Exited() {
			continue
		}
		live++
		s.mu.Lock()
		nv, seen := len(s.viewers), s.lastSeen
		s.mu.Unlock()
		if nv == 0 && canReap(id) {
			cands = append(cands, cand{s, seen})
		}
	}
	r.mu.Unlock()
	// oldest first
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].seen.Before(cands[j-1].seen); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	for _, c := range cands {
		if time.Since(c.seen) >= grace || live > max {
			c.s.Kill()
			live--
		}
	}
}

func (s *Session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.broadcast(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) broadcast(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scrollback = append(s.scrollback, b...)
	if over := len(s.scrollback) - scrollbackLimit; over > 0 {
		s.scrollback = append([]byte(nil), s.scrollback[over:]...)
	}
	s.lastSeen = time.Now()
	for v := range s.viewers {
		select {
		case v.C <- b:
		default: // slow viewer: drop it, it reconnects and replays
			delete(s.viewers, v)
			v.closeMu.Do(func() { close(v.C) })
		}
	}
}

// Inject writes herd's own output into the session's stream (not to the
// process), e.g. the push and PR lines.
func (s *Session) Inject(b []byte) { s.broadcast(b) }

// Attach registers a viewer and returns it with the scrollback so far.
func (s *Session) Attach() (*Viewer, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := &Viewer{C: make(chan []byte, 512), s: s}
	if s.exited {
		close(v.C)
	} else {
		s.viewers[v] = struct{}{}
	}
	return v, append([]byte(nil), s.scrollback...)
}

// Detach unregisters a viewer.
func (v *Viewer) Detach() {
	s := v.s
	s.mu.Lock()
	if _, ok := s.viewers[v]; ok {
		delete(s.viewers, v)
		v.closeMu.Do(func() { close(v.C) })
	}
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *Session) closeViewers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for v := range s.viewers {
		delete(s.viewers, v)
		v.closeMu.Do(func() { close(v.C) })
	}
}

// Viewers returns how many viewers are attached.
func (s *Session) Viewers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.viewers)
}

// Write sends input to the process.
func (s *Session) Write(b []byte) (int, error) {
	if s.Exited() {
		return 0, io.ErrClosedPipe
	}
	return s.pty.Write(b)
}

// Resize changes the PTY size.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || s.Exited() {
		return nil
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// ExitCode is the process's exit code once it has ended.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Exited reports whether the process has ended.
func (s *Session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// Done is closed when the output has been fully read.
func (s *Session) Done() <-chan struct{} { return s.done }

// Kill ends the process: SIGTERM, then SIGKILL after 3 seconds.
func (s *Session) Kill() {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	s.killed = true
	s.mu.Unlock()
	if s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}()
}
