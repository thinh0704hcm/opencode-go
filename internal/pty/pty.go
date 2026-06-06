package pty

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const maxBuffer = 2 * 1024 * 1024

// Pty is one pseudo-terminal session.
type Pty struct {
	ID      string
	Title   string
	Command string
	Created int64 // epoch ms
	cmd     *exec.Cmd
	ptmx    *os.File
	mu      sync.Mutex
	closed  bool
	tickets map[string]int64 // one-time connect tickets -> expiry epoch ms

	// Buffered fan-out: a single background reader owns all ptmx reads,
	// appends to a ring buffer, and copies each chunk to every subscriber.
	buffer        []byte
	cursor        int // total bytes ever written to subscribers
	subs          map[int]chan []byte
	nextSub       int
	readerStarted bool
}

// Info is the JSON-safe view returned by HTTP handlers.
type Info struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Command string `json:"command"`
	Created int64  `json:"created"`
}

func (p *Pty) Info() Info {
	return Info{
		ID:      p.ID,
		Title:   p.Title,
		Command: p.Command,
		Created: p.Created,
	}
}

// Registry manages PTY sessions.
type Registry struct {
	mu   sync.Mutex
	ptys map[string]*Pty
}

func NewRegistry() *Registry {
	return &Registry{ptys: make(map[string]*Pty)}
}

// Create starts a new pty running `command` (default the user's $SHELL or
// /bin/bash) in `cwd`. Returns the Pty or error.
func (r *Registry) Create(id, title, command, cwd string) (*Pty, error) {
	var cmd *exec.Cmd
	if command == "" {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		cmd = exec.Command(shell, "-l")
	} else {
		cmd = exec.Command("bash", "-lc", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	p := &Pty{
		ID:      id,
		Title:   title,
		Command: command,
		Created: time.Now().UnixMilli(),
		cmd:     cmd,
		ptmx:    ptmx,
		tickets: make(map[string]int64),
		subs:    make(map[int]chan []byte),
	}

	startReader(p)

	r.mu.Lock()
	r.ptys[id] = p
	r.mu.Unlock()
	return p, nil
}

// startReader launches the single background goroutine that owns all reads
// of ptmx. Each chunk is appended to the ring buffer and fanned out to every
// subscriber (non-blocking; a full subscriber drops only its own chunk). On
// read error/EOF it closes all subscriber channels so handlers exit.
func startReader(p *Pty) {
	p.mu.Lock()
	if p.readerStarted {
		p.mu.Unlock()
		return
	}
	p.readerStarted = true
	ptmx := p.ptmx
	p.mu.Unlock()

	go func() {
		defer func() {
			if p.cmd != nil {
				_ = p.cmd.Wait()
			}
		}()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				p.mu.Lock()
				p.buffer = append(p.buffer, chunk...)
				if len(p.buffer) > maxBuffer {
					p.buffer = p.buffer[len(p.buffer)-maxBuffer:]
				}
				p.cursor += n
				for _, ch := range p.subs {
					select {
					case ch <- chunk:
					default:
					}
				}
				p.mu.Unlock()
			}
			if err != nil {
				p.closeSubs()
				return
			}
		}
	}()
}

// closeSubs closes and removes all subscriber channels (idempotent per sub).
func (p *Pty) closeSubs() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for id, ch := range p.subs {
		close(ch)
		delete(p.subs, id)
	}
}

func (r *Registry) Get(id string) (*Pty, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.ptys[id]
	return p, ok
}

// List returns Info for all sessions, sorted by Created.
func (r *Registry) List() []Info {
	r.mu.Lock()
	out := make([]Info, 0, len(r.ptys))
	for _, p := range r.ptys {
		out = append(out, p.Info())
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Created < out[j].Created
	})
	return out
}

// Remove kills the process + closes the ptmx and deletes it from the map.
func (r *Registry) Remove(id string) bool {
	r.mu.Lock()
	p, ok := r.ptys[id]
	if ok {
		delete(r.ptys, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	_ = p.Close()
	return true
}

// Resize sets the terminal window size.
func (p *Pty) Resize(rows, cols uint16) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Ptmx exposes the master fd for the websocket pump (read/write).
func (p *Pty) Ptmx() *os.File {
	return p.ptmx
}

// Subscribe returns a snapshot of the current buffered bytes, a channel of
// future chunks, the current cursor, and an unsubscribe func. The unsub func
// is safe to call multiple times (double-close guarded).
func (p *Pty) Subscribe() (backlog []byte, ch <-chan []byte, cursor int, unsub func()) {
	p.mu.Lock()
	snapshot := append([]byte(nil), p.buffer...)
	cur := p.cursor
	c := make(chan []byte, 256)
	id := p.nextSub
	p.nextSub++
	if p.closed {
		// Reader already gone: deliver backlog, then signal EOF.
		close(c)
		p.mu.Unlock()
		return snapshot, c, cur, func() {}
	}
	p.subs[id] = c
	p.mu.Unlock()

	var once sync.Once
	unsub = func() {
		once.Do(func() {
			p.mu.Lock()
			if sub, ok := p.subs[id]; ok {
				delete(p.subs, id)
				close(sub)
			}
			p.mu.Unlock()
		})
	}
	return snapshot, c, cur, unsub
}

// WriteInput writes raw input bytes to the pty.
func (p *Pty) WriteInput(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

// IssueTicket mints a one-time, ~30s-TTL ticket for websocket connect auth.
func (p *Pty) IssueTicket() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	p.mu.Lock()
	p.tickets[t] = time.Now().UnixMilli() + 30_000
	p.mu.Unlock()
	return t
}

// RedeemTicket validates+consumes a ticket (single use, not expired).
func (p *Pty) RedeemTicket(t string) bool {
	now := time.Now().UnixMilli()
	p.mu.Lock()
	defer p.mu.Unlock()
	exp, ok := p.tickets[t]
	if !ok {
		return false
	}
	delete(p.tickets, t)
	return now <= exp
}

// Close kills the process group and closes the ptmx (idempotent).
func (p *Pty) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
	if p.ptmx != nil {
		return p.ptmx.Close()
	}
	return nil
}

// Shells returns common available shells (check /bin/bash, /bin/sh, $SHELL exist).
func Shells() []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		if fi, err := os.Stat(s); err == nil && !fi.IsDir() {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(os.Getenv("SHELL"))
	add("/bin/bash")
	add("/bin/sh")
	return out
}
