package main

// A Live chat connection (the hub): the merged chat of every platform
// connection, the vMix title, and the state and actions behind the Live
// chat widget.

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

// config is the Live chat connection's settings: only vMix now; each
// platform is a connection of its own.
type config struct {
	VmixHost      string `json:"vmixHost"`
	VmixTitle     string `json:"vmixTitle"`
	VmixTitleText string `json:"vmixTitleText"`
	VmixUser      string `json:"vmixUser"`
	VmixPassword  string `json:"vmixPassword"`
}

// shownLimit is how many messages the widget gets.
const shownLimit = 100

// samplePrefix starts every sample message's id.
const samplePrefix = "sample:"

// Conn is one Live chat connection (a hub): it merges every platform
// connection open in this add-on process, or plays the sample chat when
// there is none.
type Conn struct {
	cfg   config
	store *store
	vmix  *vmixClient

	mu        sync.Mutex
	feeds     map[string]feedView // open platform feeds, by feed id
	sample    bool
	sampleSt  sourceStatus
	sampleGen int
	fakeStop  func() // stops the sample chat (nil when it isn't running)
	vmixErr   string // the last vMix problem ("" fine)
	vmixOK    bool
	vmixReady chan struct{}
	waited    bool
	closed    bool

	nmu    sync.Mutex
	notify addon.Notifier

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Open starts a Live chat connection from its settings.
func Open(raw json.RawMessage) (*Conn, error) {
	var cfg config
	if err := readConfig(raw, &cfg); err != nil {
		return nil, err
	}
	vm, err := newVmix(cfg.VmixHost, cfg.VmixTitle, cfg.VmixTitleText, cfg.VmixUser, cfg.VmixPassword)
	if err != nil {
		return nil, err
	}
	c := &Conn{cfg: cfg, store: newStore(), vmix: vm, feeds: map[string]feedView{}, vmixReady: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	if vm.input != "" {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			pctx, done := context.WithTimeout(ctx, 4*time.Second)
			defer done()
			c.setVmix(vm.ping(pctx))
			close(c.vmixReady)
		}()
	} else {
		close(c.vmixReady)
	}
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
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
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
	if c.store.add(msgs...) > 0 {
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
	c.store.add(backlog...)
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

func (c *Conn) setVmix(err error) {
	c.mu.Lock()
	if err != nil {
		c.vmixErr, c.vmixOK = err.Error(), false
	} else {
		c.vmixErr, c.vmixOK = "", true
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------- state

// wireMessage is a message as the widget gets it.
type wireMessage struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Author   string `json:"author"`
	At       string `json:"at"`
	Text     string `json:"text"`
	Super    string `json:"super,omitempty"`
	OnScreen bool   `json:"onScreen,omitempty"`
}

func wire(m Message) wireMessage {
	return wireMessage{ID: m.ID, Platform: m.Platform, Author: m.Author, At: m.At.UTC().Format(time.RFC3339), Text: m.Text, Super: m.Super}
}

// wireSource is one status chip in the widget's header.
type wireSource struct {
	Platform string `json:"platform"`
	Label    string `json:"label"`
	State    string `json:"state"` // live | waiting | error
	Note     string `json:"note"`
	Viewers  int    `json:"viewers"` // -1 unknown
}

// AddonState is the Live chat widget's state.
func (c *Conn) AddonState(ctx context.Context) (map[string]any, error) {
	c.mu.Lock()
	first := !c.waited
	c.waited = true
	var waits []chan struct{}
	for _, v := range c.feeds {
		waits = append(waits, v.ready)
	}
	c.mu.Unlock()
	if first { // the first ask (often Test right after Open): let every source look once
		waits = append(waits, c.vmixReady)
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
	return c.state(), nil
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

func (c *Conn) state() map[string]any {
	v := c.store.view(shownLimit)
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
	live, bad, watching := false, false, -1
	sources := []wireSource{}
	seenName := map[string]bool{}
	for _, f := range c.chips() {
		st := f.st
		ws := wireSource{Platform: f.plat, Label: f.label, State: st.State, Note: st.Note, Viewers: st.Viewers}
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
	case !live:
		note = "Waiting for a live stream"
	}
	switch {
	case c.vmix.input == "":
		details = append(details, "vMix: no title input set (Show on screen is off)")
	case c.vmixErr != "":
		details = append(details, "vMix: "+c.vmixErr)
	case c.vmixOK:
		details = append(details, "vMix: answered")
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
		"canShow":     c.vmix.input != "",
		"screenLabel": "on screen in vMix (" + c.vmix.input + ")",
		"vmixError":   c.vmixErr,
		"detail":      strings.Join(details, " · "),
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
	defer c.changed()
	switch name {
	case "show_on_screen":
		var m Message
		var err error
		if p.ID == "" {
			var ok bool
			if m, ok = c.store.latest(); !ok {
				return nil, errors.New("there are no chat messages yet")
			}
		} else if m, err = msg(); err != nil {
			return nil, err
		}
		err = c.vmix.show(ctx, m)
		c.setVmix(err)
		if err != nil {
			return nil, err
		}
		c.store.setOnScreen(m)
		return map[string]any{"ok": true, "id": m.ID, "author": m.Author}, nil
	case "clear_screen":
		err := c.vmix.clear(ctx)
		c.setVmix(err)
		if err != nil {
			return nil, err
		}
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
