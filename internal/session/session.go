package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Session struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CWD        string    `json:"cwd"`
	Tmux       string    `json:"tmux_session"`
	ClaudeArgs []string  `json:"claude_args,omitempty"`
	Transcript string    `json:"transcript,omitempty"`
	LastOffset int64     `json:"last_offset,omitempty"`
}

type Registry struct {
	path     string
	Sessions []Session `json:"sessions"`
}

func StateDir() string {
	if v := os.Getenv("AGENTRUN_STATE_DIR"); v != "" {
		return v
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "agentrun")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "agentrun")
}

// Load returns a read-only snapshot of the registry. Mutations must go through Mutate so they
// are serialized via flock with respect to other agentrun processes.
func Load() (*Registry, error) {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{path: filepath.Join(dir, "sessions.json")}
	if err := readInto(r); err != nil {
		return nil, err
	}
	return r, nil
}

func readInto(r *Registry) error {
	b, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.Sessions = nil
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		r.Sessions = nil
		return nil
	}
	return json.Unmarshal(b, r)
}

// Mutate runs fn under an exclusive file lock so concurrent agentrun invocations cannot race
// on read-modify-write. The registry passed to fn reflects the latest on-disk state. Save is
// atomic via tmp+rename so a crash mid-write does not truncate the registry.
func Mutate(fn func(*Registry) error) error {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "sessions.json")
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock registry: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	r := &Registry{path: path}
	if err := readInto(r); err != nil {
		return err
	}
	if err := fn(r); err != nil {
		return err
	}
	return atomicSave(r)
}

func atomicSave(r *Registry) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(r.path), "sessions-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	return os.Rename(tmpName, r.path)
}

func (r *Registry) NextID() string {
	max := 0
	for _, s := range r.Sessions {
		n, err := strconv.Atoi(s.ID)
		if err == nil && n > max {
			max = n
		}
	}
	return strconv.Itoa(max + 1)
}

// Allocate appends a session to the registry under lock, assigning the next free numeric ID.
// The returned Session reflects the persisted ID. This closes the NextID/Add race that would
// occur if the caller computed NextID on a stale snapshot.
func Allocate(s Session) (Session, error) {
	var out Session
	err := Mutate(func(r *Registry) error {
		s.ID = r.NextID()
		r.Sessions = append(r.Sessions, s)
		out = s
		return nil
	})
	return out, err
}

func Update(s Session) error {
	return Mutate(func(r *Registry) error {
		for i := range r.Sessions {
			if r.Sessions[i].ID == s.ID {
				r.Sessions[i] = s
				return nil
			}
		}
		return fmt.Errorf("unknown session %s", s.ID)
	})
}

func Remove(id string) error {
	return Mutate(func(r *Registry) error {
		for i := range r.Sessions {
			if r.Sessions[i].ID == id {
				r.Sessions = append(r.Sessions[:i], r.Sessions[i+1:]...)
				return nil
			}
		}
		return nil
	})
}

func (r *Registry) Get(id string) (*Session, error) {
	for i := range r.Sessions {
		if r.Sessions[i].ID == id || r.Sessions[i].Name == id {
			return &r.Sessions[i], nil
		}
	}
	return nil, fmt.Errorf("unknown session %s", id)
}

// Last returns the most recently updated session without mutating the underlying slice order.
func (r *Registry) Last() (*Session, error) {
	if len(r.Sessions) == 0 {
		return nil, fmt.Errorf("no sessions")
	}
	idx := 0
	for i := 1; i < len(r.Sessions); i++ {
		if r.Sessions[i].UpdatedAt.After(r.Sessions[idx].UpdatedAt) {
			idx = i
		}
	}
	return &r.Sessions[idx], nil
}
