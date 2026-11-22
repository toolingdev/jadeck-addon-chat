package main

// The merged chat: messages from every platform in one list, oldest
// first, without duplicates, with the widget's own marks (pinned, on
// screen, hidden here).

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// Platforms.
const (
	YouTube  = "youtube"
	Facebook = "facebook"
	Twitch   = "twitch"
	Rumble   = "rumble"
	Owncast  = "owncast"
)

// Message is one chat message or comment.
type Message struct {
	ID       string     // "<platform>:<the platform's id>", unique across platforms
	Platform string     // YouTube, Facebook, Twitch, Rumble, Owncast (or "sample")
	Author   string     // display name
	Avatar   string     // picture URL, when the platform gives one
	Text     string     // the message (a Super Chat's comment)
	At       time.Time  // when it was posted
	Super    string     // a Super Chat / Super Sticker amount ("$10.00"), else ""
	Mine     bool       // sent from jadeck (Author is the account it went out as)
	Sent     []sentMark // Mine: where it got through
	ReplyTo  string     // Mine: the author it answered ("" a message to everyone)
	feed     string     // the platform feed it came from (registry id)
	seq      uint64     // arrival order (ties on At)
}

// sentMark is one platform under a message sent from jadeck: ✓ or ✕.
type sentMark struct {
	Platform string `json:"platform"`
	OK       bool   `json:"ok"`
}

// maxKept is how many messages the add-on keeps (the oldest go first).
const maxKept = 500

// store is the merged chat. Safe for concurrent use.
type store struct {
	mu       sync.Mutex
	msgs     []Message // sorted by At, then arrival
	byID     map[string]int
	seq      uint64
	total    int // every message ever added (kept or not)
	hidden   map[string]bool
	pinned   string // id of the pinned message ("" none)
	onScreen string // id of the message on screen in vMix ("" none)
	shown    *Message
}

func newStore() *store {
	return &store{byID: map[string]int{}, hidden: map[string]bool{}}
}

// add merges messages in: new ones are inserted in time order, ones
// already there are skipped. It reports how many were new.
func (s *store) add(in ...Message) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range in {
		if m.ID == "" || strings.TrimSpace(m.Text) == "" && m.Super == "" {
			continue
		}
		if _, dup := s.byID[m.ID]; dup {
			continue
		}
		s.seq++
		m.seq = s.seq
		if m.At.IsZero() {
			m.At = time.Now()
		}
		// Usually the newest: append, then walk it back into place.
		s.msgs = append(s.msgs, m)
		for i := len(s.msgs) - 1; i > 0 && s.msgs[i].At.Before(s.msgs[i-1].At); i-- {
			s.msgs[i], s.msgs[i-1] = s.msgs[i-1], s.msgs[i]
		}
		s.byID[m.ID] = -1
		s.total++
		n++
	}
	if n == 0 {
		return 0
	}
	if over := len(s.msgs) - maxKept; over > 0 {
		for _, m := range s.msgs[:over] {
			if m.ID != s.pinned && m.ID != s.onScreen {
				delete(s.hidden, m.ID)
			}
		}
		s.msgs = slices.Clone(s.msgs[over:])
	}
	s.reindex()
	return n
}

// remove drops a message (deleted on the platform).
func (s *store) remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byID[id]
	if !ok || i < 0 {
		return false
	}
	s.msgs = slices.Delete(s.msgs, i, i+1)
	s.reindex()
	if s.pinned == id && s.onScreen != id {
		s.pinned = ""
	}
	return true
}

// reindex rebuilds byID; ids that were trimmed stay known (as -1) so a
// platform repeating an old message doesn't bring it back.
func (s *store) reindex() {
	for k := range s.byID {
		s.byID[k] = -1
	}
	for i, m := range s.msgs {
		s.byID[m.ID] = i
	}
	if len(s.byID) > 4*maxKept {
		keep := map[string]int{}
		for i, m := range s.msgs {
			keep[m.ID] = i
		}
		s.byID = keep
	}
}

// get returns a message by id.
func (s *store) get(id string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id)
}

func (s *store) getLocked(id string) (Message, bool) {
	if i, ok := s.byID[id]; ok && i >= 0 {
		return s.msgs[i], true
	}
	if s.shown != nil && s.shown.ID == id {
		return *s.shown, true
	}
	return Message{}, false
}

// latest is the newest message not hidden here.
func (s *store) latest() (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.msgs) - 1; i >= 0; i-- {
		if !s.hidden[s.msgs[i].ID] {
			return s.msgs[i], true
		}
	}
	return Message{}, false
}

func (s *store) hide(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hidden[id] = true
	if s.pinned == id && s.onScreen != id {
		s.pinned = ""
	}
}

func (s *store) unhideAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hidden = map[string]bool{}
}

func (s *store) pin(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinned = id
	delete(s.hidden, id)
}

// unpin clears the pin (the message on screen stays pinned while it's up).
func (s *store) unpin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinned = s.onScreen
}

// setOnScreen marks the message vMix shows; it's pinned while it's up.
func (s *store) setOnScreen(m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onScreen, s.pinned = m.ID, m.ID
	s.shown = &m
	delete(s.hidden, m.ID)
}

func (s *store) clearScreen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pinned == s.onScreen {
		s.pinned = ""
	}
	s.onScreen, s.shown = "", nil
}

// removePrefix drops every message whose id starts with prefix (the
// sample chat, when a real platform arrives), and takes them out of the
// total too. The ids stay known, so they don't come back.
func (s *store) removePrefix(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.msgs[:0:0]
	n := 0
	for _, m := range s.msgs {
		if strings.HasPrefix(m.ID, prefix) {
			n++
			delete(s.hidden, m.ID)
			continue
		}
		keep = append(keep, m)
	}
	if n == 0 {
		return 0
	}
	s.msgs = keep
	s.total -= n
	if strings.HasPrefix(s.pinned, prefix) && s.pinned != s.onScreen {
		s.pinned = "" // one still up in vMix stays pinned until cleared
	}
	s.reindex()
	return n
}

// hasOther reports whether any kept message's id doesn't start with prefix.
func (s *store) hasOther(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if !strings.HasPrefix(m.ID, prefix) {
			return true
		}
	}
	return false
}

// suppress makes ids known without showing them: a platform's echo of a
// message sent from here (it's already in the chat as "YOU").
func (s *store) suppress(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if _, ok := s.byID[id]; !ok && id != "" {
			s.byID[id] = -1
		}
	}
}

// setSent updates a sent message's ✓ / ✕ marks.
func (s *store) setSent(id string, marks []sentMark) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byID[id]
	if !ok || i < 0 {
		return false
	}
	s.msgs[i].Sent = slices.Clone(marks)
	return true
}

// view is what the widget draws.
type view struct {
	messages       []Message // the newest `limit` not hidden (and not the pinned one), oldest first
	pinned         *Message
	onScreen       bool
	total, hiddenN int
}

func (s *store) view(limit int) view {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := view{total: s.total}
	if s.pinned != "" {
		if m, ok := s.getLocked(s.pinned); ok {
			v.pinned, v.onScreen = &m, s.onScreen == s.pinned
		}
	}
	for _, m := range s.msgs {
		if s.hidden[m.ID] {
			v.hiddenN++
		}
	}
	for i := len(s.msgs) - 1; i >= 0 && len(v.messages) < limit; i-- {
		m := s.msgs[i]
		if s.hidden[m.ID] || m.ID == s.pinned {
			continue
		}
		v.messages = append(v.messages, m)
	}
	slices.Reverse(v.messages)
	return v
}
