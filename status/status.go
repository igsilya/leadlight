package status

import (
	"sync"
	"time"
)

type Key string

const (
	Sync            Key = "sync"
	BgSync          Key = "bgsync"
	Comments        Key = "comments"
	BgComments      Key = "bgcomments"
	BgCoverComments Key = "bgcovercomments"
	Detail          Key = "detail"
	Archive         Key = "archive"
	Update          Key = "update"
	Info            Key = "info"
)

type entry struct {
	message   string
	spinner   bool
	updatedAt time.Time
	expireAt  time.Time
}

// fetchEntry tracks an in-progress fetch operation. Multiple goroutines
// can fetch the same item simultaneously (e.g., comments and details),
// so refCount tracks how many are active. The entry is removed when
// refCount reaches zero.
type fetchEntry struct {
	seriesID int // series this item belongs to, for IsFetchingSeries lookups
	refCount int // number of concurrent fetches for this item
}

type Registry struct {
	mu      sync.Mutex
	entries map[Key]entry
	// activeFetches maps item IDs (patch or cover) to their fetch
	// state. Used to show per-row spinners in the TUI while
	// background goroutines fetch comments or details.
	activeFetches map[int]fetchEntry
	onChange      func()
}

func NewRegistry(onChange func()) *Registry {
	return &Registry{
		entries:       map[Key]entry{},
		activeFetches: map[int]fetchEntry{},
		onChange:      onChange,
	}
}

func (r *Registry) Set(key Key, msg string, spinner bool) {
	r.mu.Lock()
	r.entries[key] = entry{
		message: msg, spinner: spinner,
		updatedAt: time.Now(),
	}
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *Registry) SetTimed(
	key Key, msg string, d time.Duration,
) {
	r.mu.Lock()
	now := time.Now()
	r.entries[key] = entry{
		message: msg, updatedAt: now,
		expireAt: now.Add(d),
	}
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *Registry) Clear(key Key) {
	r.mu.Lock()
	delete(r.entries, key)
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *Registry) StartFetchAndSetStatus(
	itemID, seriesID int, key Key, msg string,
) {
	r.mu.Lock()
	fe := r.activeFetches[itemID]
	fe.seriesID = seriesID
	fe.refCount++
	r.activeFetches[itemID] = fe
	r.entries[key] = entry{
		message: msg, spinner: true, updatedAt: time.Now(),
	}
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *Registry) EndFetch(itemID int) {
	r.mu.Lock()
	if fe, ok := r.activeFetches[itemID]; ok {
		fe.refCount--
		if fe.refCount <= 0 {
			delete(r.activeFetches, itemID)
		} else {
			r.activeFetches[itemID] = fe
		}
	}
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

func (r *Registry) IsFetchingPatch(patchID int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.activeFetches[patchID]
	return ok
}

func (r *Registry) IsFetchingSeries(seriesID int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, fe := range r.activeFetches {
		if fe.seriesID == seriesID {
			return true
		}
	}
	return false
}

func (r *Registry) HasActiveFetches() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.activeFetches) > 0
}

func (r *Registry) Active() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for key, e := range r.entries {
		if !e.expireAt.IsZero() && now.After(e.expireAt) {
			delete(r.entries, key)
		}
	}
	var latest entry
	found := false
	for _, e := range r.entries {
		if !found || e.updatedAt.After(latest.updatedAt) {
			latest = e
			found = true
		}
	}
	if !found {
		return "", len(r.activeFetches) > 0
	}
	return latest.message, latest.spinner || len(r.activeFetches) > 0
}
