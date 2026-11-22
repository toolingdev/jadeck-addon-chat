package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const rumbleTestKey = "rk_secret_0123456789"

// rumbleFake is the Live Stream API.
type rumbleFake struct {
	mu     sync.Mutex
	live   bool
	status int // answer with this status instead
	polls  int
	extra  bool // a newer message in the next answer
}

func (f *rumbleFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != rumbleTestKey {
			t.Errorf("key %q", r.URL.Query().Get("key"))
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls++
		if f.status != 0 {
			w.WriteHeader(f.status)
			fmt.Fprintf(w, `{"error":"bad key %s"}`, rumbleTestKey)
			return
		}
		live := "false"
		if f.live {
			live = "true"
		}
		extra := ""
		if f.extra {
			extra = `,{"username":"Late","text":"just joined","created_on":"2026-11-21T19:45:00+00:00"}`
		}
		fmt.Fprintf(w, `{"user_id":"u1","username":"yourchurch","channel_name":"Your Church","livestreams":[
		  {"id":"old","title":"Last week","is_live":false,"watching_now":0,"chat":{"recent_messages":[],"recent_rants":[]}},
		  {"id":"v1","title":"Sunday service","is_live":%s,"watching_now":8,"chat":{
		    "recent_messages":[
		      {"username":"Ruth","badges":[],"text":"Good morning","created_on":"2026-11-21T19:41:00+00:00"},
		      {"username":"Pedro","text":"For the fund","created_on":"2026-11-21T19:42:00+00:00"},
		      {"username":"Ana","text":"Amen","created_on":"2026-11-21T19:43:00+00:00"}%s],
		    "recent_rants":[
		      {"username":"Pedro","text":"For the fund","created_on":"2026-11-21T19:42:00+00:00","expires_on":"2026-11-21T19:52:00+00:00","amount_cents":1000},
		      {"username":"Tom","text":"Praying","created_on":"2026-11-21T19:44:00+00:00","amount_dollars":2.5}]}}]}`, live, extra)
	})
}

func testRumble(t *testing.T, f *rumbleFake) *rumble {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	rb, err := newRumble("https://rumble.com/-livestream-api/get-data?key=" + rumbleTestKey)
	if err != nil {
		t.Fatal(err)
	}
	rb.link = srv.URL + "/-livestream-api/get-data?key=" + rumbleTestKey
	rb.liveEvery, rb.idleEvery = 10*time.Millisecond, 30*time.Millisecond
	return rb
}

func TestNewRumbleChecksTheLink(t *testing.T) {
	for _, ok := range []string{"https://rumble.com/-livestream-api/get-data?key=abc", " https://www.rumble.com/x?key=1 "} {
		if _, err := newRumble(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://rumble.com/x?key=1", "https://rumble.com.evil.example/x", "https://notrumble.com/x", "rumble.com/x"} {
		if _, err := newRumble(bad); err == nil {
			t.Errorf("%q accepted", bad)
		} else if strings.Contains(err.Error(), "key=1") {
			t.Errorf("error repeats the link: %v", err)
		}
	}
}

func TestRumbleReadsChatAndRants(t *testing.T) {
	f := &rumbleFake{live: true}
	rb := testRumble(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { rb.run(ctx, out); close(done) }()
	waitFor(t, "four messages", func() bool { return out.s.view(20).total == 4 })
	st := out.last()
	if st.State != stLive || st.Viewers != 8 || st.Title != "Sunday service" || st.Label != "Your Church" {
		t.Fatalf("status %+v", st)
	}
	// Pedro's message is also his rant: one message, with the amount
	var supers []string
	for _, m := range out.s.view(20).messages {
		supers = append(supers, m.Author+"="+m.Super)
	}
	if got := strings.Join(supers, " "); got != "Ruth= Pedro=$10.00 Ana= Tom=$2.50" {
		t.Fatalf("messages %s", got)
	}
	// later polls: the same 50 again add nothing; a new one comes in once
	f.mu.Lock()
	f.extra = true
	f.mu.Unlock()
	waitFor(t, "the late message", func() bool { return out.s.view(20).total == 5 })
	n := out.count()
	time.Sleep(60 * time.Millisecond)
	if out.s.view(20).total != 5 {
		t.Fatal("a repeat was added")
	}
	if more := out.count() - n; more != 0 {
		t.Fatalf("%d status reports with nothing changed", more)
	}
	// the stream ends
	f.mu.Lock()
	f.live = false
	f.mu.Unlock()
	waitFor(t, "waiting", func() bool { return out.last().State == stWaiting })
	cancel()
	<-done
}

func TestRumbleExpiredLinkNeverShowsTheKey(t *testing.T) {
	f := &rumbleFake{status: 403}
	rb := testRumble(t, f)
	_, err := rb.fetch(context.Background())
	if err != errRumbleLink {
		t.Fatalf("403: %v", err)
	}
	f.status = 500
	_, err = rb.fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), rumbleTestKey) {
		t.Fatalf("500: %v", err)
	}
	dead, _ := newRumble("https://rumble.com/-livestream-api/get-data?key=" + rumbleTestKey)
	dead.link = "http://127.0.0.1:1/-livestream-api/get-data?key=" + rumbleTestKey
	_, err = dead.fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), rumbleTestKey) || strings.Contains(err.Error(), "livestream-api") {
		t.Fatalf("transport error leaks the link: %v", err)
	}
	// run says so in plain words
	f.status = 401
	ctx, cancel := context.WithCancel(context.Background())
	out := newRecSink()
	done := make(chan struct{})
	go func() { rb.run(ctx, out); close(done) }()
	waitFor(t, "the error", func() bool { return out.last().State == stError })
	if n := out.last().Note; !strings.Contains(n, "expired or is wrong") || strings.Contains(n, rumbleTestKey) {
		t.Fatalf("note %q", n)
	}
	cancel()
	<-done
}

func TestRumbleSeenSetIsBounded(t *testing.T) {
	rb, _ := newRumble("https://rumble.com/x?key=k")
	var items []rumbleChatItem
	for i := 0; i < rumbleSeenMax+100; i++ {
		items = append(items, rumbleChatItem{Username: "u", Text: fmt.Sprint("m", i), CreatedOn: "2026-11-21T19:41:00+00:00"})
	}
	if n := len(rb.fresh(items, false)); n != rumbleSeenMax+100 {
		t.Fatalf("fresh %d", n)
	}
	if len(rb.seen) != rumbleSeenMax || len(rb.order) != rumbleSeenMax {
		t.Fatalf("seen %d order %d", len(rb.seen), len(rb.order))
	}
	if n := len(rb.fresh(items[len(items)-10:], false)); n != 0 {
		t.Fatalf("recent repeats came back: %d", n)
	}
}
