package cli

import "testing"

func TestCleanupManagerRunsOnce(t *testing.T) {
	var calls int
	c := &cleanupManager{}
	c.Set(func() { calls++ })
	c.Run()
	c.Run()
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestCleanupManagerCanBeSetBeforeRun(t *testing.T) {
	c := &cleanupManager{}
	if !c.Empty() {
		t.Fatal("new cleanup manager should be empty")
	}
	ran := false
	c.Set(func() { ran = true })
	if c.Empty() {
		t.Fatal("cleanup manager should not be empty after Set")
	}
	c.Run()
	if !ran {
		t.Fatal("cleanup function did not run")
	}
}
