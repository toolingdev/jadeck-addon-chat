package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// ctlSource is a platform source the test drives.
type ctlSource struct {
	plat string
	in   chan Message
	mu   sync.Mutex
	runs int
	live bool
}

func newCtl(plat string) *ctlSource { return &ctlSource{plat: plat, in: make(chan Message, 16)} }

func (s *ctlSource) platform() string { return s.plat }
func (s *ctlSource) run(ctx context.Context, out sink) {
	s.mu.Lock()
	s.runs++
	s.live = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.live = false; s.mu.Unlock() }()
	out.status(sourceStatus{Platform: s.plat, State: stLive, Viewers: 7})
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-s.in:
			out.add(m)
		}
	}
}
func (s *ctlSource) running() (int, bool) { s.mu.Lock(); defer s.mu.Unlock(); return s.runs, s.live }

func hubState(t *testing.T, c *Conn) map[string]any {
	t.Helper()
	st, err := c.AddonState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func msgIDs(st map[string]any) string {
	var out []string
	for _, m := range st["messages"].([]wireMessage) {
		out = append(out, m.ID)
	}
	return strings.Join(out, " ")
}

func chipList(st map[string]any) string {
	var out []string
	for _, s := range st["sources"].([]wireSource) {
		out = append(out, s.Platform+"/"+s.Label+"/"+s.State)
	}
	return strings.Join(out, " ")
}

func pm(plat, id string, sec int) Message {
	return Message{ID: plat + ":" + id, Platform: plat, Author: "A", Text: "t " + id, At: t0.Add(time.Duration(sec) * time.Second)}
}

func TestHubAddsAndRemovesPlatformsWithoutRestarts(t *testing.T) {
	a, err := Open(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if st := hubState(t, a); st["sample"] != true || chipList(st) != "sample/Sample chat/live" || len(st["messages"].([]wireMessage)) != 5 {
		t.Fatalf("sample: %v / %s", st["sample"], chipList(st))
	}

	// a Twitch connection: sample off, its chat in
	tw := newCtl(Twitch)
	p1 := openPlatform(tw, "yourchurch")
	tw.in <- pm(Twitch, "1", 1)
	waitFor(t, "twitch:1 in hub a", func() bool { _, ok := a.store.get("twitch:1"); return ok })
	st := hubState(t, a)
	if st["sample"] != false || msgIDs(st) != "twitch:1" || chipList(st) != "twitch/yourchurch/live" || st["source"] != "TWITCH" {
		t.Fatalf("after twitch: sample %v, %q, %s, %v", st["sample"], msgIDs(st), chipList(st), st["source"])
	}

	// a second hub opened later gets what Twitch already said
	b, _ := Open(nil)
	defer b.Close()
	if st := hubState(t, b); st["sample"] != false || msgIDs(st) != "twitch:1" {
		t.Fatalf("late hub: %v %q", st["sample"], msgIDs(st))
	}

	// add YouTube: Twitch keeps running (one run), both hubs get both
	yt := newCtl(YouTube)
	p2 := openPlatform(yt, "@yourchannel")
	yt.in <- pm(YouTube, "2", 2)
	tw.in <- pm(Twitch, "3", 3)
	for _, h := range []*Conn{a, b} {
		waitFor(t, "both platforms", func() bool { return msgIDs(hubState(t, h)) == "twitch:1 youtube:2 twitch:3" })
	}
	if runs, live := tw.running(); runs != 1 || !live {
		t.Fatalf("twitch runs %d live %v", runs, live)
	}
	st = hubState(t, a)
	if chipList(st) != "twitch/yourchurch/live youtube/@yourchannel/live" || st["source"] != "TWITCH + YOUTUBE" || st["watching"] != 14 {
		t.Fatalf("chips %s source %v watching %v", chipList(st), st["source"], st["watching"])
	}
	if d := st["detail"].(string); !strings.Contains(d, "Twitch (yourchurch): live (7 watching)") || !strings.Contains(d, "YouTube (@yourchannel)") {
		t.Fatalf("detail %q", d)
	}

	// pin a Twitch message, then remove the Twitch connection: its chip
	// goes, its messages and the pin stay, YouTube runs on
	if _, err := a.AddonAction(context.Background(), "pin", json.RawMessage(`{"id":"twitch:1"}`)); err != nil {
		t.Fatal(err)
	}
	p1.Close()
	if _, live := tw.running(); live {
		t.Fatal("twitch still running after Close")
	}
	yt.in <- pm(YouTube, "4", 4)
	waitFor(t, "youtube:4", func() bool { _, ok := a.store.get("youtube:4"); return ok })
	st = hubState(t, a)
	if chipList(st) != "youtube/@yourchannel/live" || msgIDs(st) != "youtube:2 twitch:3 youtube:4" {
		t.Fatalf("after removing twitch: %s / %q", chipList(st), msgIDs(st))
	}
	if p, _ := st["pinned"].(wireMessage); p.ID != "twitch:1" {
		t.Fatalf("pin lost: %v", st["pinned"])
	}
	if runs, live := yt.running(); runs != 1 || !live {
		t.Fatalf("youtube runs %d live %v", runs, live)
	}
	// Test's state for the platform connection
	ps, _ := p2.AddonState(context.Background())
	if ps["detail"] != "YouTube (@yourchannel): live (7 watching)" {
		t.Fatalf("platform detail %v", ps["detail"])
	}

	// the last platform goes: the history stays, no sample mixed in
	p2.Close()
	st = hubState(t, a)
	if st["sample"] != false || chipList(st) != "" || msgIDs(st) != "youtube:2 twitch:3 youtube:4" || !strings.Contains(st["note"].(string), "No platform connection") {
		t.Fatalf("no platforms: sample %v chips %q msgs %q note %v", st["sample"], chipList(st), msgIDs(st), st["note"])
	}
}

func TestHubBackToSampleWhenNothingRealCame(t *testing.T) {
	a, _ := Open(nil)
	defer a.Close()
	src := newCtl(Owncast)
	p := openPlatform(src, "live.example.org")
	waitFor(t, "sample off", func() bool { return hubState(t, a)["sample"] == false })
	if st := hubState(t, a); msgIDs(st) != "" {
		t.Fatalf("sample messages left: %q", msgIDs(st))
	}
	p.Close()
	st := hubState(t, a)
	if st["sample"] != true || len(st["messages"].([]wireMessage)) != 5 {
		t.Fatalf("sample back: %v, %d messages", st["sample"], len(st["messages"].([]wireMessage)))
	}
}

// A Test of a platform connection briefly opens a copy: one chip, not two.
func TestHubMergesDuplicateChips(t *testing.T) {
	a, _ := Open(nil)
	defer a.Close()
	p1 := openPlatform(newCtl(Rumble), "Your Church")
	p2 := openPlatform(newCtl(Rumble), "your church")
	waitFor(t, "both live", func() bool { return chipList(hubState(t, a)) == "rumble/Your Church/live" })
	p2.Close()
	p1.Close()
}
