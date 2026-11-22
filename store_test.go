package main

import (
	"fmt"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 21, 19, 0, 0, 0, time.UTC)

func msg(id string, sec int) Message {
	return Message{ID: id, Platform: YouTube, Author: "A " + id, Text: "text " + id, At: t0.Add(time.Duration(sec) * time.Second)}
}

func ids(ms []Message) string {
	s := ""
	for _, m := range ms {
		s += m.ID + " "
	}
	return s
}

func TestStoreMergesInTimeOrderWithoutDuplicates(t *testing.T) {
	s := newStore()
	if n := s.add(msg("yt:1", 10), msg("fb:1", 5), msg("yt:2", 20)); n != 3 {
		t.Fatalf("added %d", n)
	}
	// a late Facebook comment lands between, repeats are dropped
	if n := s.add(msg("fb:2", 15), msg("yt:1", 10), msg("fb:1", 5)); n != 1 {
		t.Fatalf("added %d, want 1 (the rest are repeats)", n)
	}
	v := s.view(100)
	if got := ids(v.messages); got != "fb:1 yt:1 fb:2 yt:2 " {
		t.Fatalf("order %q", got)
	}
	if v.total != 4 {
		t.Fatalf("total %d", v.total)
	}
	// same second: arrival order
	s.add(msg("yt:3", 20))
	if got := ids(s.view(2).messages); got != "yt:2 yt:3 " {
		t.Fatalf("tie order / limit %q", got)
	}
	// empty messages are skipped, a Super Chat without a comment isn't
	if s.add(Message{ID: "yt:empty", Text: "  "}) != 0 {
		t.Fatal("empty message kept")
	}
	if s.add(Message{ID: "yt:sc", Super: "$5.00", At: t0.Add(time.Minute)}) != 1 {
		t.Fatal("super chat without text dropped")
	}
}

func TestStoreTrimsOldestAndRemembersIDs(t *testing.T) {
	s := newStore()
	for i := 0; i < maxKept+20; i++ {
		s.add(msg(fmt.Sprint("yt:", i), i))
	}
	v := s.view(maxKept * 2)
	if len(v.messages) != maxKept || v.messages[0].ID != "yt:20" || v.total != maxKept+20 {
		t.Fatalf("kept %d from %s, total %d", len(v.messages), v.messages[0].ID, v.total)
	}
	// an old one coming again (a platform repeating its backlog) stays out
	if s.add(msg("yt:3", 3)) != 0 {
		t.Fatal("trimmed message came back")
	}
}

func TestStorePinHideAndScreen(t *testing.T) {
	s := newStore()
	s.add(msg("a", 1), msg("b", 2), msg("c", 3))
	s.hide("b")
	v := s.view(10)
	if ids(v.messages) != "a c " || v.hiddenN != 1 {
		t.Fatalf("hidden: %q %d", ids(v.messages), v.hiddenN)
	}
	if m, _ := s.latest(); m.ID != "c" {
		t.Fatalf("latest %s", m.ID)
	}
	s.pin("a")
	v = s.view(10)
	if v.pinned == nil || v.pinned.ID != "a" || v.onScreen || ids(v.messages) != "c " {
		t.Fatalf("pinned: %+v on=%v list %q", v.pinned, v.onScreen, ids(v.messages))
	}
	// showing one on screen pins it (and unhides it)
	b, _ := s.get("b")
	s.setOnScreen(b)
	v = s.view(10)
	if v.pinned.ID != "b" || !v.onScreen || v.hiddenN != 0 {
		t.Fatalf("on screen: %+v on=%v hidden %d", v.pinned, v.onScreen, v.hiddenN)
	}
	s.unpin() // can't unpin what's on screen
	if v = s.view(10); v.pinned == nil || v.pinned.ID != "b" {
		t.Fatal("unpinned the message on screen")
	}
	s.clearScreen()
	if v = s.view(10); v.pinned != nil || v.onScreen {
		t.Fatalf("after clear: %+v", v.pinned)
	}
	s.unhideAll()
	s.hide("a")
	if !s.remove("c") || s.remove("c") {
		t.Fatal("remove")
	}
	if v = s.view(10); ids(v.messages) != "b " {
		t.Fatalf("after remove: %q", ids(v.messages))
	}
}

func TestStoreKeepsShownMessageAfterTrim(t *testing.T) {
	s := newStore()
	s.add(msg("first", 0))
	m, _ := s.get("first")
	s.setOnScreen(m)
	for i := 1; i <= maxKept+5; i++ {
		s.add(msg(fmt.Sprint("n", i), i))
	}
	v := s.view(5)
	if v.pinned == nil || v.pinned.ID != "first" || !v.onScreen {
		t.Fatal("the message on screen fell out of the pin")
	}
}
