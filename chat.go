package main

// The Live chat hub: an automatic connection (the controller makes it;
// it has no settings) that merges every platform connection, and the
// state and actions behind the Live chat and Live chat + reply widgets.
// Show on screen is the reply widget's own: its frame fills a title
// through the user's vMix connection and then marks the message here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// shownLimit is how many messages the widget gets.
const shownLimit = 100

// samplePrefix starts every sample message's id.
const samplePrefix = "sample:"

// Conn is the Live chat hub: it merges every platform connection open
// in this add-on process, or plays the sample chat when there is none.
type Conn struct {
	store *store

	mu        sync.Mutex
	feeds     map[string]feedView // open platform feeds, by feed id
	sample    bool
	sampleSt  sourceStatus
	sampleGen int
	fakeStop  func() // stops the sample chat (nil when it isn't running)
	waited    bool
	closed    bool

	// replies (reply.go)
	sends     map[string]*sendRecord
	sendOrder []string
	lastSend  *sendRecord
	echoes    []echo

	nmu    sync.Mutex
	notify addon.Notifier
}

// Open starts the hub. It has no settings (an Auto connection type); an
// old saved config is ignored.
func Open(json.RawMessage) (*Conn, error) {
	c := &Conn{store: newStore(), feeds: map[string]feedView{}, sends: map[string]*sendRecord{}}
	reg.subscribe(c)
	c.startSample() // only when no platform connection is open
	return c, nil
}

// Close stops the sample chat and leaves the hub.
func (c *Conn) Close() error {
	reg.unsubscribe(c)
	c.mu.Lock()
	c.closed = true
	stop := c.fakeStop
	c.fakeStop = nil
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	return nil
}

// SetNotify (addon.Notifier): the controller hears when the chat changes.
func (c *Conn) SetNotify(n addon.Notifier) {
	c.nmu.Lock()
	c.notify = n
	c.nmu.Unlock()
}

func (c *Conn) changed() {
	c.nmu.Lock()
	n := c.notify
	c.nmu.Unlock()
	if n != nil {
		n()
	}
}

// ---------------------------------------------------------- sample chat

// startSample plays the sample chat, when no platform connection is open.
func (c *Conn) startSample() {
	c.mu.Lock()
	if c.closed || c.fakeStop != nil || len(c.feeds) > 0 {
		c.mu.Unlock()
		return
	}
	c.sample = true
	c.sampleGen++
	f := newFake()
	f.gen = c.sampleGen
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.fakeStop = func() { cancel(); <-done }
	c.mu.Unlock()
	f.prime(sampleSink{c}) // the backlog is there for the first look
	go func() {
		defer close(done)
		f.run(ctx, sampleSink{c})
	}()
}

// stopSample ends the sample chat and takes its messages out.
func (c *Conn) stopSample() {
	c.mu.Lock()
	stop := c.fakeStop
	c.fakeStop, c.sample = nil, false
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	c.store.removePrefix(samplePrefix)
}

// sampleSink is where the sample chat goes (straight into this hub).
type sampleSink struct{ c *Conn }

func (s sampleSink) add(msgs ...Message) { s.c.add(msgs...) }
func (s sampleSink) remove(id string)    { s.c.remove(id) }
func (s sampleSink) status(st sourceStatus) {
	s.c.mu.Lock()
	s.c.sampleSt = st
	s.c.mu.Unlock()
	s.c.changed()
}

// ------------------------------------------------------------ the hub

func (c *Conn) add(msgs ...Message) {
	if c.store.add(c.dropEchoes(msgs)...) > 0 {
		c.changed()
	}
}

func (c *Conn) remove(id string) {
	if c.store.remove(id) {
		c.changed()
	}
}

func (c *Conn) feedAdded(v feedView, backlog []Message) {
	c.mu.Lock()
	c.feeds[v.id] = v
	leave := c.sample
	c.mu.Unlock()
	if leave {
		c.stopSample()
	}
	c.store.add(c.dropEchoes(backlog)...)
	c.changed()
}

func (c *Conn) feedMessages(msgs []Message) { c.add(msgs...) }
func (c *Conn) feedRemove(id string)        { c.remove(id) }

func (c *Conn) feedStatus(v feedView) {
	c.mu.Lock()
	if _, ok := c.feeds[v.id]; ok {
		c.feeds[v.id] = v
	}
	c.mu.Unlock()
	c.changed()
}

// feedGone: a platform connection was removed. Its messages and the pins
// stay; with no platform left and nothing real in the chat, the sample
// chat comes back.
func (c *Conn) feedGone(id string) {
	c.mu.Lock()
	delete(c.feeds, id)
	none := len(c.feeds) == 0
	c.mu.Unlock()
	if none && !c.store.hasOther(samplePrefix) {
		c.startSample()
	}
	c.changed()
}

// ---------------------------------------------------------------- state

// wireMessage is a message as the widget gets it.
type wireMessage struct {
	ID       string     `json:"id"`
	Platform string     `json:"platform"`
	Author   string     `json:"author"`
	At       string     `json:"at"`
	Text     string     `json:"text"`
	Super    string     `json:"super,omitempty"`
	Avatar   string     `json:"avatar,omitempty"` // http(s) only (Show on screen's FromPhoto)
	OnScreen bool       `json:"onScreen,omitempty"`
	Mine     bool       `json:"mine,omitempty"`    // sent from jadeck ("YOU · <account>")
	Sent     []sentMark `json:"sent,omitempty"`    // mine: ✓ / ✕ per platform
	ReplyTo  string     `json:"replyTo,omitempty"` // mine: the author it answered
}

func wire(m Message) wireMessage {
	w := wireMessage{ID: m.ID, Platform: m.Platform, Author: m.Author, At: m.At.UTC().Format(time.RFC3339), Text: m.Text, Super: m.Super,
		Mine: m.Mine, Sent: m.Sent, ReplyTo: m.ReplyTo}
	if strings.HasPrefix(m.Avatar, "https://") || strings.HasPrefix(m.Avatar, "http://") {
		w.Avatar = m.Avatar
	}
	return w
}

// wireSource is one status chip in the widget's header.
type wireSource struct {
	Platform string `json:"platform"`
	Label    string `json:"label"`
	State    string `json:"state"` // live | waiting | error
	Note     string `json:"note"`
	Viewers  int    `json:"viewers"`      // -1 unknown
	ID       string `json:"id,omitempty"` // the feed (send_message's platforms)
	CanReply bool   `json:"canReply"`
	ReplyAs  string `json:"replyAs,omitempty"`  // the account replies go out as
	ReplyWhy string `json:"replyWhy,omitempty"` // why it can't ("not signed in", "read only")
}

// AddonState is the hub's state with every platform (Test, and a widget
// that sends no settings).
func (c *Conn) AddonState(ctx context.Context) (map[string]any, error) {
	return c.AddonStateFor(ctx, nil)
}

// AddonStateFor (addon.SettingsStater) is one widget's state: only the
// platforms its "platforms" field names (empty: all).
func (c *Conn) AddonStateFor(ctx context.Context, settings map[string]any) (map[string]any, error) {
	c.firstWait(ctx)
	return c.state(platformFilter(settings)), nil
}

// firstWait: the first ask (often Test right after Open) lets every
// source look once.
func (c *Conn) firstWait(ctx context.Context) {
	c.mu.Lock()
	first := !c.waited
	c.waited = true
	var waits []chan struct{}
	for _, v := range c.feeds {
		waits = append(waits, v.ready)
	}
	c.mu.Unlock()
	if first {
		t := time.NewTimer(6 * time.Second)
	wait:
		for _, ch := range waits {
			select {
			case <-ch:
			case <-t.C:
				break wait
			case <-ctx.Done():
				break wait
			}
		}
		t.Stop()
	}
}

// filter is the platforms one widget shows; nil shows every one.
type filter map[string]bool

// platformFilter reads a widget's "platforms" setting: a list (or a
// comma list) of platform ids. Nothing known in it: every platform.
func platformFilter(settings map[string]any) filter {
	var names []string
	switch v := settings["platforms"].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
	case []string:
		names = v
	case string:
		names = strings.Split(v, ",")
	}
	f := filter{}
	for _, n := range names {
		if n = strings.ToLower(strings.TrimSpace(n)); platformNames[n] != "" && n != "sample" {
			f[n] = true
		}
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

// has: the widget shows this platform.
func (f filter) has(plat string) bool { return f == nil || f[plat] }

// keeps: the widget shows this message. One sent from jadeck shows where
// it went to one of the widget's platforms (or while it's still going).
func (f filter) keeps(m Message) bool {
	if f == nil {
		return true
	}
	if m.Mine {
		if len(m.Sent) == 0 {
			return true
		}
		for _, s := range m.Sent {
			if f[s.Platform] {
				return true
			}
		}
		return false
	}
	return f[m.Platform]
}

// platforms is the filter as a list (nil: all).
func (f filter) platforms() []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f))
	for p := range f {
		out = append(out, p)
	}
	return out
}

// chipsFor is chips() on the widget's platforms only; c.mu held.
func (c *Conn) chipsFor(f filter) []feedView {
	out := c.chips()
	if f != nil {
		out = slices.DeleteFunc(out, func(v feedView) bool { return !f.has(v.plat) })
	}
	return out
}

// chips is the open feeds, oldest first, one per platform + label (a
// Test of a platform connection briefly opens a second copy).
func (c *Conn) chips() []feedView {
	vs := make([]feedView, 0, len(c.feeds))
	for _, v := range c.feeds {
		vs = append(vs, v)
	}
	slices.SortFunc(vs, func(a, b feedView) int { return int(a.seq) - int(b.seq) })
	var out []feedView
	at := map[string]int{}
	for _, v := range vs {
		k := v.plat + "\x00" + strings.ToLower(v.label)
		if i, dup := at[k]; dup {
			if !out[i].seen || (out[i].st.State != stLive && v.seen && v.st.State == stLive) {
				v.seq, v.id = out[i].seq, out[i].id
				out[i] = v
			}
			continue
		}
		at[k] = len(out)
		out = append(out, v)
	}
	return out
}

func (c *Conn) state(f filter) map[string]any {
	var keep func(Message) bool
	if f != nil {
		keep = f.keeps
	}
	v := c.store.viewFor(shownLimit, keep, f.platforms())
	msgs := make([]wireMessage, 0, len(v.messages))
	for _, m := range v.messages {
		msgs = append(msgs, wire(m))
	}
	var pinned any
	if v.pinned != nil {
		w := wire(*v.pinned)
		w.OnScreen = v.onScreen
		pinned = w
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	var names, details, notes []string
	live, bad, watching, canSend := false, false, -1, false
	sources := []wireSource{}
	seenName := map[string]bool{}
	for _, f := range c.chipsFor(f) {
		st := f.st
		ws := wireSource{Platform: f.plat, Label: f.label, State: st.State, Note: st.Note, Viewers: st.Viewers, ID: f.id, ReplyWhy: "read only"}
		if f.rep != nil {
			ws.CanReply, ws.ReplyAs, ws.ReplyWhy = f.rep.canReply()
			if ws.CanReply {
				ws.ReplyWhy = ""
			}
			canSend = canSend || ws.CanReply
		}
		if !f.seen {
			ws.State, ws.Note, ws.Viewers = stWaiting, "checking…", -1
		}
		sources = append(sources, ws)
		if n := strings.ToUpper(platformNames[f.plat]); !seenName[n] {
			seenName[n] = true
			names = append(names, n)
		}
		details = append(details, detailLine(f))
		switch {
		case !f.seen:
		case st.State == stLive:
			live = true
			if st.Viewers >= 0 {
				watching = max(watching, 0) + st.Viewers
			}
			if st.Note != "" && !st.Info {
				notes, bad = append(notes, st.Note), true
			}
		case st.State == stError:
			bad = true
			notes = append(notes, st.Note)
		}
	}
	// One platform's problem shouldn't take over the widget while others
	// work: then it stays on its own chip and error line.
	ok := false
	for _, ws := range sources {
		if ws.State == stLive || ws.State == stWaiting {
			ok = true
		}
	}
	if ok {
		bad, notes = false, nil
	}
	source := strings.Join(names, " + ")
	note := ""
	switch {
	case c.sample:
		source = "SAMPLE CHAT"
		live = true
		watching = c.sampleSt.Viewers
		sources = []wireSource{{Platform: "sample", Label: "Sample chat", State: stLive, Viewers: c.sampleSt.Viewers}}
		details = []string{"No platform connection yet: showing a sample chat"}
	case len(notes) > 0:
		note = notes[0]
	case len(c.feeds) == 0:
		note = "No platform connection open (add YouTube, Facebook, Twitch, Rumble or Owncast chat)"
		details = []string{"No platform connection open"}
	case len(sources) == 0:
		note = "None of this widget's platforms is connected"
		details = []string{"No connection for this widget's platforms"}
	case !live:
		note = "Waiting for a live stream"
	}
	st := map[string]any{
		"connected":   true,
		"sample":      c.sample,
		"source":      source,
		"sources":     sources,
		"live":        live,
		"bad":         bad,
		"note":        note,
		"total":       v.total,
		"messages":    msgs,
		"pinned":      pinned,
		"hiddenCount": v.hiddenN,
		"detail":      strings.Join(details, " · "),
		"canSend":     canSend && !c.sample,
		"lastSend":    c.lastSendState(),
	}
	if watching >= 0 {
		st["watching"] = watching
	} else {
		st["watching"] = nil
	}
	return st
}

// --------------------------------------------------------------- actions

// AddonAction runs the widget's actions.
func (c *Conn) AddonAction(ctx context.Context, name string, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("unreadable params: %v", err)
		}
	}
	msg := func() (Message, error) {
		if p.ID == "" {
			return Message{}, errors.New("which message? (id)")
		}
		m, ok := c.store.get(p.ID)
		if !ok {
			return Message{}, errors.New("that message is no longer in the chat")
		}
		return m, nil
	}
	switch name {
	case "send_message":
		return c.sendMessage(ctx, params)
	case "retry_send":
		defer c.changed()
		return c.retrySend(ctx, params)
	case "dismiss_send":
		var d struct {
			SendID string `json:"sendId"`
		}
		json.Unmarshal(params, &d)
		c.mu.Lock()
		if c.lastSend != nil && (d.SendID == "" || d.SendID == c.lastSend.ID) {
			c.lastSend = nil
		}
		c.mu.Unlock()
		c.changed()
		return map[string]any{"ok": true}, nil
	}
	defer c.changed()
	switch name {
	case "mark_on_screen":
		// The reply widget filled the vMix title itself (through the
		// user's vMix connection); every screen now marks it ON SCREEN.
		m, err := msg()
		if err != nil {
			return nil, err
		}
		c.store.setOnScreen(m)
		return map[string]any{"ok": true, "id": m.ID}, nil
	case "clear_on_screen":
		c.store.clearScreen()
		return map[string]any{"ok": true}, nil
	case "pin":
		m, err := msg()
		if err != nil {
			return nil, err
		}
		c.store.pin(m.ID)
		return map[string]any{"ok": true}, nil
	case "unpin":
		c.store.unpin()
		return map[string]any{"ok": true}, nil
	case "hide":
		m, err := msg()
		if err != nil {
			return nil, err
		}
		c.store.hide(m.ID)
		return map[string]any{"ok": true}, nil
	case "show_hidden":
		c.store.unhideAll()
		return map[string]any{"ok": true}, nil
	}
	return nil, fmt.Errorf("%w: %s", addon.ErrUnknownAction, name)
}
