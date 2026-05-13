package session

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

func withTempStateDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AGENTRUN_STATE_DIR", dir)
}

func TestAllocateAssignsSequentialIDs(t *testing.T) {
	withTempStateDir(t)
	a, err := Allocate(Session{Name: "a", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Allocate(Session{Name: "b", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "1" || b.ID != "2" {
		t.Fatalf("ids: a=%s b=%s", a.ID, b.ID)
	}
}

func TestAllocateConcurrent(t *testing.T) {
	withTempStateDir(t)
	const n = 16
	var wg sync.WaitGroup
	ids := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Allocate(Session{CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err != nil {
				t.Errorf("allocate: %v", err)
				return
			}
			ids <- s.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id under concurrency: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique ids, got %d", n, len(seen))
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Sessions) != n {
		t.Fatalf("registry has %d sessions, want %d", len(reg.Sessions), n)
	}
}

func TestUpdateAndRemove(t *testing.T) {
	withTempStateDir(t)
	s, err := Allocate(Session{CreatedAt: time.Now(), UpdatedAt: time.Now(), CWD: "/old"})
	if err != nil {
		t.Fatal(err)
	}
	s.CWD = "/new"
	if err := Update(s); err != nil {
		t.Fatal(err)
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != "/new" {
		t.Fatalf("cwd: got %s want /new", got.CWD)
	}
	if err := Remove(s.ID); err != nil {
		t.Fatal(err)
	}
	reg, _ = Load()
	if len(reg.Sessions) != 0 {
		t.Fatalf("expected empty registry, got %d", len(reg.Sessions))
	}
}

func TestLastDoesNotMutateOrder(t *testing.T) {
	withTempStateDir(t)
	base := time.Now()
	for i := 0; i < 3; i++ {
		s, err := Allocate(Session{CreatedAt: base, UpdatedAt: base.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		_ = s
	}
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	idsBefore := make([]string, len(reg.Sessions))
	for i, s := range reg.Sessions {
		idsBefore[i] = s.ID
	}
	last, err := reg.Last()
	if err != nil {
		t.Fatal(err)
	}
	if last.ID != "3" {
		t.Fatalf("last id: got %s want 3", last.ID)
	}
	if !sort.StringsAreSorted(idsBefore) {
		t.Fatalf("test bug: ids should be sorted ascending, got %v", idsBefore)
	}
	idsAfter := make([]string, len(reg.Sessions))
	for i, s := range reg.Sessions {
		idsAfter[i] = s.ID
	}
	for i := range idsBefore {
		if idsBefore[i] != idsAfter[i] {
			t.Fatalf("Last() mutated order: before=%v after=%v", idsBefore, idsAfter)
		}
	}
}

func TestRegistryFilePermissions(t *testing.T) {
	withTempStateDir(t)
	_, err := Allocate(Session{CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(StateDir() + "/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("sessions.json perms: got %#o want 0600", mode)
	}
}

func TestMutateErrorDoesNotPersist(t *testing.T) {
	withTempStateDir(t)
	want := fmt.Errorf("boom")
	err := Mutate(func(r *Registry) error {
		r.Sessions = append(r.Sessions, Session{ID: "99", CreatedAt: time.Now(), UpdatedAt: time.Now()})
		return want
	})
	if err == nil {
		t.Fatal("expected error")
	}
	reg, _ := Load()
	if len(reg.Sessions) != 0 {
		t.Fatalf("error path should not persist; got %d sessions", len(reg.Sessions))
	}
}
