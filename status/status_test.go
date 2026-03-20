package status

import (
	"sync"
	"testing"
	"time"
)

func TestRegistry_SetAndActive(t *testing.T) {
	r := NewRegistry(nil)
	r.Set(Sync, "Syncing...", true)

	msg, spinner := r.Active()
	if msg != "Syncing..." {
		t.Errorf("msg = %q", msg)
	}
	if !spinner {
		t.Error("should be spinner")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry(nil)
	r.Set(Sync, "Syncing...", true)
	r.Clear(Sync)

	msg, _ := r.Active()
	if msg != "" {
		t.Errorf("msg = %q, want empty", msg)
	}
}

func TestRegistry_TimedExpiry(t *testing.T) {
	r := NewRegistry(nil)
	r.SetTimed(Info, "Done", 0)
	time.Sleep(time.Millisecond)

	msg, _ := r.Active()
	if msg != "" {
		t.Errorf("msg = %q, want empty (expired)", msg)
	}
}

func TestRegistry_MostRecent(t *testing.T) {
	r := NewRegistry(nil)
	r.Set(BgSync, "Checking events...", true)
	time.Sleep(time.Millisecond)
	r.Set(Detail, "Fetching details...", true)

	msg, _ := r.Active()
	if msg != "Fetching details..." {
		t.Errorf("msg = %q, want most recent", msg)
	}
}

func TestRegistry_SpinnerFlag(t *testing.T) {
	r := NewRegistry(nil)
	r.SetTimed(Info, "Wrote file", 10*time.Second)

	_, spinner := r.Active()
	if spinner {
		t.Error("timed entry should not be spinner")
	}
}

func TestRegistry_OnChange(t *testing.T) {
	called := 0
	r := NewRegistry(func() { called++ })
	r.Set(Sync, "test", true)
	r.Clear(Sync)
	r.SetTimed(Info, "done", time.Second)

	if called != 3 {
		t.Errorf("onChange called %d times, want 3", called)
	}
}

func TestRegistry_NilOnChange(t *testing.T) {
	r := NewRegistry(nil)
	r.Set(Sync, "test", true)
	r.Clear(Sync)
}

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry(nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Set(Detail, "working...", true)
				r.Active()
				r.Clear(Detail)
			}
		}()
	}
	wg.Wait()
}
