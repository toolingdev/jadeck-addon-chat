package main

// The in-process hub: every platform connection (YouTube, Facebook,
// Twitch, Rumble, Owncast) open in this add-on process registers its
// source here as a feed, and the Live chat hub (the automatic "chat"
// connection; in tests, several) subscribes. A source runs once and fans
// out to all hubs; adding or removing one platform connection never
// restarts the others.
//
// Everything a feed tells the hubs is delivered while holding reg.mu, so
// a hub sees a feed's arrival, messages, statuses and departure in order
// (a late status can't bring a removed chip back). Hub methods called
// from here must never take reg.mu.

import (
	"fmt"
	"slices"
	"sync"
)

// feed is one registered platform source: its last status and its recent
// messages (replayed to a hub that subscribes later).
type feed struct {
	id, plat, label string
	seq             uint64
	ready           chan struct{} // closed at the first status
	rep             replier       // nil: this platform can't reply (Rumble)
	gate            sendGate      // at most one send a second

	// guarded by reg.mu
	st         sourceStatus
	seen       bool
	recent     []Message
	registered bool
}

func newFeed(plat, label string) *feed {
	return &feed{plat: plat, label: label, ready: make(chan struct{})}
}

// feedView is a feed as a hub keeps it.
type feedView struct {
	id, plat, label string
	seq             uint64
	st              sourceStatus
	seen            bool
	ready           chan struct{}
	rep             replier
	gate            *sendGate
}

// view is the feed's current look (reg.mu held).
func (f *feed) view() feedView {
	v := feedView{id: f.id, plat: f.plat, label: f.label, seq: f.seq, st: f.st, seen: f.seen, ready: f.ready, rep: f.rep, gate: &f.gate}
	if f.st.Label != "" {
		v.label = f.st.Label
	}
	return v
}

// hub is what the Live chat hub does for the registry.
type hub interface {
	feedAdded(v feedView, backlog []Message)
	feedMessages(msgs []Message)
	feedRemove(id string)
	feedStatus(v feedView)
	feedGone(id string)
}

type registry struct {
	mu    sync.Mutex
	seq   uint64
	feeds map[string]*feed
	hubs  []hub
}

var reg = &registry{feeds: map[string]*feed{}}

// register adds a feed (under a new unique id) and tells every hub.
func (r *registry) register(f *feed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	f.seq, f.id, f.registered = r.seq, fmt.Sprintf("%s-%d", f.plat, r.seq), true
	r.feeds[f.id] = f
	for _, h := range r.hubs {
		h.feedAdded(f.view(), nil)
	}
}

// unregister removes a feed; hubs drop its chip and keep its messages.
func (r *registry) unregister(f *feed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !f.registered {
		return
	}
	f.registered = false
	delete(r.feeds, f.id)
	for _, h := range r.hubs {
		h.feedGone(f.id)
	}
}

// subscribe adds a hub and hands it every feed open now, with what each
// has already received.
func (r *registry) subscribe(h hub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hubs = append(r.hubs, h)
	for _, f := range r.sortedLocked() {
		h.feedAdded(f.view(), slices.Clone(f.recent))
	}
}

func (r *registry) unsubscribe(h hub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hubs = slices.DeleteFunc(r.hubs, func(x hub) bool { return x == h })
}

// views lists the open feeds, oldest first.
func (r *registry) views() []feedView {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []feedView
	for _, f := range r.sortedLocked() {
		out = append(out, f.view())
	}
	return out
}

func (r *registry) sortedLocked() []*feed {
	fs := make([]*feed, 0, len(r.feeds))
	for _, f := range r.feeds {
		fs = append(fs, f)
	}
	slices.SortFunc(fs, func(a, b *feed) int { return int(a.seq) - int(b.seq) })
	return fs
}

// ------------------------------------------------- a feed is a sink

func (f *feed) add(msgs ...Message) {
	if len(msgs) == 0 {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	msgs = slices.Clone(msgs)
	for i := range msgs {
		msgs[i].feed = f.id
	}
	f.recent = append(f.recent, msgs...)
	if over := len(f.recent) - maxKept; over > 0 {
		f.recent = slices.Clone(f.recent[over:])
	}
	if !f.registered {
		return
	}
	for _, h := range reg.hubs {
		h.feedMessages(msgs)
	}
}

func (f *feed) remove(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	f.recent = slices.DeleteFunc(f.recent, func(m Message) bool { return m.ID == id })
	if !f.registered {
		return
	}
	for _, h := range reg.hubs {
		h.feedRemove(id)
	}
}

func (f *feed) status(st sourceStatus) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	f.st = st
	if !f.seen {
		f.seen = true
		close(f.ready)
	}
	if !f.registered {
		return
	}
	for _, h := range reg.hubs {
		h.feedStatus(f.view())
	}
}

// current is the feed's look now.
func (f *feed) current() feedView {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return f.view()
}

// touch tells every hub the feed's look again (its reply sign-in changed).
func (r *registry) touch(f *feed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !f.registered {
		return
	}
	for _, h := range r.hubs {
		h.feedStatus(f.view())
	}
}
