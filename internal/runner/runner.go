// Package runner owns one PTY per live step: start, resume, input, resize,
// scrollback replay for viewers, and reaping idle sessions. With Hold set,
// each PTY lives in a holder process (see package holder) so the session
// survives a herd restart.
package runner

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"herd/internal/holder"
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

// Session is one live process in a PTY, local or in a holder.
type Session struct {
	StepID string
	cmd    *exec.Cmd
	pty    *os.File // local PTY
	conn   net.Conn // holder connection
	wmu    sync.Mutex

	mu         sync.Mutex
	scrollback []byte
	viewers    map[*Viewer]struct{}
	lastSeen   time.Time // last time a viewer detached or output arrived
	killed     bool
	detached   bool // herd let go of the holder; the process keeps running
	exited     bool
	exitCode   int
	heldCode   int // exit code sent by the holder
	gotExit    bool
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
	// Hold is the holder command (e.g. herd's own path and "hold"); empty
	// runs each process in a local PTY that dies with herd.
	Hold    []string
	SockDir string // holder sockets

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
	if len(r.Hold) > 0 {
		if cmd.Err != nil {
			return nil, cmd.Err
		}
		return r.startHeld(spec.StepID, cmd)
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return nil, err
	}
	s := newSession(spec.StepID)
	s.cmd, s.pty = cmd, f
	go s.readLoop()
	r.track(s, func() int {
		err := cmd.Wait()
		f.Close()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return ee.ExitCode()
			}
			return -1
		}
		return 0
	})
	return s, nil
}

func newSession(stepID string) *Session {
	return &Session{StepID: stepID, viewers: map[*Viewer]struct{}{}, lastSeen: time.Now(), done: make(chan struct{})}
}

// track registers s and, once wait returns the exit code, retires it.
func (r *Runner) track(s *Session, wait func() int) {
	r.mu.Lock()
	r.sessions[s.StepID] = s
	r.mu.Unlock()
	go func() {
		code := wait()
		s.mu.Lock()
		s.exited, s.exitCode = true, code
		killed, detached := s.killed, s.detached
		s.mu.Unlock()
		<-s.done // wait for the reader to drain
		s.closeViewers()
		r.mu.Lock()
		if r.sessions[s.StepID] == s {
			delete(r.sessions, s.StepID)
		}
		if !detached {
			r.remember(s)
		}
		r.mu.Unlock()
		if r.onExit != nil && !detached {
			r.onExit(s.StepID, code, killed)
		}
	}()
}

func (r *Runner) sock(stepID string) string {
	sum := sha1.Sum([]byte(stepID))
	return filepath.Join(r.SockDir, hex.EncodeToString(sum[:8])+".sock")
}

// startHeld runs cmd inside a new holder process and connects to it.
func (r *Runner) startHeld(stepID string, cmd *exec.Cmd) (*Session, error) {
	if s, err := r.Attach(stepID); err == nil {
		return s, ErrRunning // a holder from an earlier run still has this step
	}
	if err := os.MkdirAll(r.SockDir, 0o700); err != nil {
		return nil, err
	}
	sock := r.sock(stepID)
	args := append(append([]string{}, r.Hold[1:]...), sock, "--", cmd.Path)
	h := exec.Command(r.Hold[0], append(args, cmd.Args[1:]...)...)
	h.Dir, h.Env = cmd.Dir, cmd.Env
	h.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives herd
	if err := h.Start(); err != nil {
		return nil, err
	}
	go func() { _ = h.Wait() }() // reap it if it ends while herd runs
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			return r.held(stepID, conn), nil
		}
		if time.Now().After(deadline) {
			_ = h.Process.Kill()
			return nil, errors.New("the session holder did not start: " + err.Error())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Attach reconnects to a step's holder left running by an earlier herd.
func (r *Runner) Attach(stepID string) (*Session, error) {
	if len(r.Hold) == 0 {
		return nil, errors.New("sessions are not held")
	}
	if s, ok := r.Get(stepID); ok {
		return s, nil
	}
	conn, err := net.Dial("unix", r.sock(stepID))
	if err != nil {
		return nil, err
	}
	return r.held(stepID, conn), nil
}

func (r *Runner) held(stepID string, conn net.Conn) *Session {
	s := newSession(stepID)
	s.conn = conn
	first := make(chan struct{})
	go s.readFrames(first)
	select { // the holder's first frame is the scrollback: have it before viewers attach
	case <-first:
	case <-time.After(2 * time.Second):
	}
	r.track(s, func() int {
		<-s.done
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gotExit {
			return s.heldCode
		}
		return -1 // the holder went away without a word
	})
	return s
}

// readFrames reads a holder's output until it closes.
func (s *Session) readFrames(first chan struct{}) {
	defer close(s.done)
	var once sync.Once
	defer once.Do(func() { close(first) })
	for {
		kind, p, err := holder.ReadFrame(s.conn)
		if err != nil {
			return
		}
		switch kind {
		case holder.Output:
			if len(p) > 0 {
				s.broadcast(p)
			}
		case holder.Exit:
			code, _ := strconv.Atoi(string(p))
			s.mu.Lock()
			s.heldCode, s.gotExit = code, true
			s.mu.Unlock()
		}
		once.Do(func() { close(first) })
	}
}

func (s *Session) send(kind byte, p []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return holder.WriteFrame(s.conn, kind, p)
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

// Shutdown lets go of the sessions keep approves (their holders keep the
// process running for the next herd) and ends every other one.
func (r *Runner) Shutdown(keep func(stepID string) bool) {
	r.mu.Lock()
	var kill []*Session
	for id, s := range r.sessions {
		if s.conn != nil && keep(id) {
			s.mu.Lock()
			s.detached = true
			s.mu.Unlock()
			s.conn.Close()
		} else {
			kill = append(kill, s)
		}
	}
	r.mu.Unlock()
	for _, s := range kill {
		s.Kill()
	}
	for _, s := range kill {
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
		}
	}
}

// StopAll ends every session.
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
	if s.conn != nil {
		if err := s.send(holder.Input, b); err != nil {
			return 0, err
		}
		return len(b), nil
	}
	return s.pty.Write(b)
}

// Resize changes the PTY size.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || s.Exited() {
		return nil
	}
	if s.conn != nil {
		return s.send(holder.Resize, holder.ResizePayload(cols, rows))
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
	if s.conn != nil {
		_ = s.send(holder.Kill, nil) // the holder escalates to SIGKILL itself
		return
	}
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
