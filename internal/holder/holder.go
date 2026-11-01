// Package holder keeps a step's Claude Code process alive across herd
// restarts. herd runs itself as `herd hold <socket> -- <cmd...>`: a small
// detached process that owns the PTY, keeps the scrollback, and serves both
// on a unix socket. herd connects as a client; when herd quits it only
// disconnects, and the next herd reconnects and replays the output.
package holder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Frame kinds. Each frame is kind (1 byte), length (uint32 big endian), payload.
const (
	Output = 'o' // holder → client: terminal output (the first one is the scrollback)
	Exit   = 'x' // holder → client: the process ended; payload is the exit code
	Input  = 'i' // client → holder: keystrokes
	Resize = 'r' // client → holder: cols, rows as two uint16
	Kill   = 'k' // client → holder: end the process
)

const scrollbackLimit = 2 << 20

// WriteFrame writes one frame.
func WriteFrame(w io.Writer, kind byte, p []byte) error {
	_, err := w.Write(encode(kind, p))
	return err
}

// ReadFrame reads one frame.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(h[1:])
	if n > 64<<20 {
		return 0, nil, errors.New("frame too large")
	}
	p := make([]byte, n)
	_, err := io.ReadFull(r, p)
	return h[0], p, err
}

// ResizePayload encodes a Resize frame.
func ResizePayload(cols, rows int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b, uint16(cols))
	binary.BigEndian.PutUint16(b[2:], uint16(rows))
	return b
}

type client struct {
	conn    net.Conn
	out     chan []byte   // encoded frames
	flushed chan struct{} // closed once out is drained and conn closed
}

type holder struct {
	mu         sync.Mutex
	scrollback []byte
	clients    map[*client]struct{}
	pty        *os.File
	cmd        *exec.Cmd
	killOnce   sync.Once
}

// Main runs a holder: args are <socket> -- <bin> <args...>. The command
// inherits the holder's environment and working directory.
func Main(args []string) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "usage: herd hold <socket> -- <command> [args...]")
		return 2
	}
	sock := args[0]
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT, syscall.SIGPIPE)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.Remove(sock)
	_ = os.Chmod(sock, 0o600)

	cmd := exec.Command(args[2], args[3:]...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		ln.Close()
		return 1
	}
	h := &holder{clients: map[*client]struct{}{}, pty: f, cmd: cmd}
	go h.accept(ln)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				h.output(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	select { // drain what the process wrote last
	case <-readDone:
	case <-time.After(500 * time.Millisecond):
	}
	f.Close()
	ln.Close()
	h.finish(code)
	return 0
}

func (h *holder) output(p []byte) {
	frame := encode(Output, p)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scrollback = append(h.scrollback, p...)
	if over := len(h.scrollback) - scrollbackLimit; over > 0 {
		h.scrollback = append([]byte(nil), h.scrollback[over:]...)
	}
	for c := range h.clients {
		select {
		case c.out <- frame:
		default: // too slow: drop it; herd reconnects and replays
			delete(h.clients, c)
			close(c.out)
		}
	}
}

func (h *holder) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		c := &client{conn: conn, out: make(chan []byte, 1024), flushed: make(chan struct{})}
		h.mu.Lock()
		c.out <- encode(Output, h.scrollback)
		h.clients[c] = struct{}{}
		h.mu.Unlock()
		go func() {
			defer close(c.flushed)
			for frame := range c.out {
				if _, err := conn.Write(frame); err != nil {
					break
				}
			}
			conn.Close()
		}()
		go h.serve(c)
	}
}

// serve handles one client's input frames.
func (h *holder) serve(c *client) {
	defer func() {
		h.mu.Lock()
		if _, ok := h.clients[c]; ok {
			delete(h.clients, c)
			close(c.out)
		}
		h.mu.Unlock()
	}()
	for {
		kind, p, err := ReadFrame(c.conn)
		if err != nil {
			return
		}
		switch kind {
		case Input:
			_, _ = h.pty.Write(p)
		case Resize:
			if len(p) == 4 {
				_ = pty.Setsize(h.pty, &pty.Winsize{Cols: binary.BigEndian.Uint16(p), Rows: binary.BigEndian.Uint16(p[2:])})
			}
		case Kill:
			h.kill()
		}
	}
}

// kill ends the process: SIGTERM, then SIGKILL after 3 seconds.
func (h *holder) kill() {
	h.killOnce.Do(func() {
		if h.cmd.Process == nil {
			return
		}
		_ = h.cmd.Process.Signal(syscall.SIGTERM)
		go func() {
			time.Sleep(3 * time.Second)
			_ = h.cmd.Process.Kill()
		}()
	})
}

// finish tells every client the exit code and waits for them to receive it.
func (h *holder) finish(code int) {
	frame := encode(Exit, []byte(fmt.Sprint(code)))
	h.mu.Lock()
	var flushed []chan struct{}
	for c := range h.clients {
		select {
		case c.out <- frame:
		default:
		}
		close(c.out)
		flushed = append(flushed, c.flushed)
		delete(h.clients, c)
	}
	h.mu.Unlock()
	timeout := time.After(2 * time.Second)
	for _, f := range flushed {
		select {
		case <-f:
		case <-timeout:
			return
		}
	}
}

func encode(kind byte, p []byte) []byte {
	b := make([]byte, 5+len(p))
	b[0] = kind
	binary.BigEndian.PutUint32(b[1:5], uint32(len(p)))
	copy(b[5:], p)
	return b
}
