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

func TestStartEndFetch(t *testing.T) {
	r := NewRegistry(nil)
	if r.IsFetchingPatch(100) {
		t.Error("should not be fetching before start")
	}
	r.StartFetchAndSetStatus(100, 50, BgComments, "fetching...")
	if !r.IsFetchingPatch(100) {
		t.Error("should be fetching after start")
	}
	r.EndFetch(100)
	if r.IsFetchingPatch(100) {
		t.Error("should not be fetching after end")
	}
}

func TestIsFetchingSeries(t *testing.T) {
	r := NewRegistry(nil)
	r.StartFetchAndSetStatus(100, 50, BgComments, "fetching...")
	if !r.IsFetchingSeries(50) {
		t.Error("series 50 should be fetching")
	}
	if r.IsFetchingSeries(99) {
		t.Error("series 99 should not be fetching")
	}
	r.EndFetch(100)
	if r.IsFetchingSeries(50) {
		t.Error("series 50 should not be fetching after end")
	}
}

func TestHasActiveFetches(t *testing.T) {
	r := NewRegistry(nil)
	if r.HasActiveFetches() {
		t.Error("should have no active fetches initially")
	}
	r.StartFetchAndSetStatus(100, 50, BgComments, "fetching...")
	if !r.HasActiveFetches() {
		t.Error("should have active fetches")
	}
	r.EndFetch(100)
	if r.HasActiveFetches() {
		t.Error("should have no active fetches after end")
	}
}

func TestStartFetch_SetsStatus(t *testing.T) {
	r := NewRegistry(nil)
	r.StartFetchAndSetStatus(100, 50, BgComments, "Fetching comments...")
	msg, spinner := r.Active()
	if msg != "Fetching comments..." {
		t.Errorf("msg = %q, want Fetching comments...", msg)
	}
	if !spinner {
		t.Error("should be spinner")
	}
}

func TestStartFetch_OnChange(t *testing.T) {
	called := 0
	r := NewRegistry(func() { called++ })
	r.StartFetchAndSetStatus(100, 50, BgComments, "fetching...")
	r.EndFetch(100)
	if called != 2 {
		t.Errorf("onChange called %d times, want 2", called)
	}
}

func TestMultipleFetches_SameSeries(t *testing.T) {
	r := NewRegistry(nil)
	r.StartFetchAndSetStatus(100, 50, BgComments, "comments...")
	r.StartFetchAndSetStatus(101, 50, Detail, "details...")
	if !r.IsFetchingSeries(50) {
		t.Error("series 50 should be fetching")
	}
	r.EndFetch(100)
	if !r.IsFetchingSeries(50) {
		t.Error("series 50 should still be fetching (101 active)")
	}
	r.EndFetch(101)
	if r.IsFetchingSeries(50) {
		t.Error("series 50 should not be fetching after all ended")
	}
}

func TestMultipleFetches_SameItem(t *testing.T) {
	r := NewRegistry(nil)
	r.StartFetchAndSetStatus(100, 50, BgComments, "comments...")
	r.StartFetchAndSetStatus(100, 50, Detail, "details...")
	if !r.IsFetchingPatch(100) {
		t.Error("patch 100 should be fetching")
	}
	r.EndFetch(100)
	if !r.IsFetchingPatch(100) {
		t.Error("patch 100 should still be fetching (refCount=1)")
	}
	r.EndFetch(100)
	if r.IsFetchingPatch(100) {
		t.Error("patch 100 should not be fetching after both ended")
	}
}

func TestActiveFetches_SpinnerLifecycle(t *testing.T) {
	r := NewRegistry(nil)
	_, spinning := r.Active()
	if spinning {
		t.Error("should not be spinning with no fetches")
	}
	r.StartFetchAndSetStatus(100, 50, BgComments, "fetching...")
	_, spinning = r.Active()
	if !spinning {
		t.Error("should be spinning with active fetch")
	}
	r.EndFetch(100)
	r.Clear(BgComments)
	_, spinning = r.Active()
	if spinning {
		t.Error("should not be spinning after fetch ended and status cleared")
	}
}

func TestNextExpiry_NoTimedEntries(t *testing.T) {
	r := NewRegistry(nil)
	r.Set(BgComments, "fetching...", true)
	if d := r.NextExpiry(); d != 0 {
		t.Errorf("NextExpiry = %v, want 0 (no timed entries)", d)
	}
}

func TestNextExpiry_SingleTimed(t *testing.T) {
	r := NewRegistry(nil)
	r.SetTimed(Info, "done", 3*time.Second)
	d := r.NextExpiry()
	if d <= 0 || d > 3*time.Second {
		t.Errorf("NextExpiry = %v, want 0 < d <= 3s", d)
	}
}

func TestNextExpiry_AlreadyExpired(t *testing.T) {
	r := NewRegistry(nil)
	r.SetTimed(Info, "old", 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	d := r.NextExpiry()
	if d != time.Millisecond {
		t.Errorf("NextExpiry = %v, want 1ms (immediate)", d)
	}
}

func TestNextExpiry_MultipleTimed(t *testing.T) {
	r := NewRegistry(nil)
	r.SetTimed(Info, "soon", 1*time.Second)
	r.SetTimed(BgComments, "later", 5*time.Second)
	d := r.NextExpiry()
	if d <= 0 || d > 1*time.Second {
		t.Errorf("NextExpiry = %v, want <= 1s (soonest)", d)
	}
}

func TestNextExpiry_Empty(t *testing.T) {
	r := NewRegistry(nil)
	if d := r.NextExpiry(); d != 0 {
		t.Errorf("NextExpiry = %v, want 0 (empty registry)", d)
	}
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
