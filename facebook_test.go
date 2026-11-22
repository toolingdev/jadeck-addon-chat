package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "EAAGtestPageToken987654321"

type fbFake struct {
	mu     sync.Mutex
	live   bool
	ended  bool
	since  []string
	polls  int
	badTok bool
}

func (f *fbFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if q.Get("access_token") != testToken || f.badTok {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"Error validating access token: Session has expired.","type":"OAuthException","code":190}}`))
			return
		}
		switch r.URL.Path {
		case "/1234567890/live_videos":
			if q.Get("broadcast_status") != `["LIVE"]` {
				t.Errorf("broadcast_status %q", q.Get("broadcast_status"))
			}
			if !f.live {
				w.Write([]byte(`{"data":[]}`))
				return
			}
			w.Write([]byte(`{"data":[{"id":"LV1","title":"Service","status":"LIVE","live_views":13}]}`))
		case "/LV1/comments":
			if q.Get("order") != "reverse_chronological" || q.Get("live_filter") != "no_filter" {
				t.Errorf("comments query %s", r.URL.RawQuery)
			}
			f.polls++
			f.since = append(f.since, q.Get("since"))
			if f.polls == 1 {
				w.Write([]byte(fbPage1))
				return
			}
			// the same newest comment again (since overlaps) and one new
			w.Write([]byte(`{"data":[
				{"id":"LV1_c3","message":"Saludos desde San Antonio","created_time":"2026-11-21T19:46:00+0000","from":{"id":"9","name":"Luis P."}},
				{"id":"LV1_c2","message":"Amén!","created_time":"2026-11-21T19:45:00+0000"}]}`))
		case "/LV1":
			if f.ended {
				w.Write([]byte(`{"id":"LV1","status":"VOD"}`))
				return
			}
			w.Write([]byte(`{"id":"LV1","status":"LIVE","live_views":15}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

const fbPage1 = `{"data":[
 {"id":"LV1_c2","message":"Amén!","created_time":"2026-11-21T19:45:00+0000"},
 {"id":"LV1_c1","message":"¿Pueden subir el volumen?","created_time":"2026-11-21T19:43:10+0000","from":{"id":"7","name":"Ana L.","picture":{"data":{"url":"https://fb.example/ana.jpg"}}}}
],"paging":{"cursors":{"before":"x","after":"y"}}}`

func TestParseFacebookComments(t *testing.T) {
	var r fbComments
	if err := json.Unmarshal([]byte(fbPage1), &r); err != nil {
		t.Fatal(err)
	}
	ms := parseFacebookComments(r)
	if len(ms) != 2 {
		t.Fatalf("%d comments", len(ms))
	}
	if m := ms[1]; m.ID != "facebook:LV1_c1" || m.Author != "Ana L." || m.Avatar != "https://fb.example/ana.jpg" ||
		!m.At.Equal(time.Date(2026, 11, 21, 19, 43, 10, 0, time.UTC)) || m.Platform != Facebook {
		t.Fatalf("comment %+v", m)
	}
	// no "from" (the token can't see who): a plain name
	if ms[0].Author != "Facebook user" || ms[0].Text != "Amén!" {
		t.Fatalf("anonymous %+v", ms[0])
	}
}

func testFacebook(t *testing.T, f *fbFake) *facebook {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	fb, err := newFacebook("1234567890", testToken)
	if err != nil {
		t.Fatal(err)
	}
	fb.base, fb.hc = srv.URL, srv.Client()
	return fb
}

func TestFacebookFindLiveAndBadToken(t *testing.T) {
	f := &fbFake{}
	fb := testFacebook(t, f)
	if _, err := fb.findLive(context.Background()); err != errNotLive {
		t.Fatalf("nothing live: %v", err)
	}
	f.live = true
	lv, err := fb.findLive(context.Background())
	if err != nil || lv.ID != "LV1" || lv.viewers() != 13 {
		t.Fatalf("live %+v %v", lv, err)
	}
	f.badTok = true
	_, err = fb.findLive(context.Background())
	if fbCode(err) != 190 || !strings.Contains(fb.problem(err), "Page access token") {
		t.Fatalf("bad token: %v / %s", err, fb.problem(err))
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("token in the error")
	}
}

func TestFacebookTransportErrorHasNoToken(t *testing.T) {
	fb, _ := newFacebook("1234567890", testToken)
	fb.base = "http://127.0.0.1:1" // nothing listens
	_, err := fb.findLive(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "access_token") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestNewFacebookChecksSettings(t *testing.T) {
	if _, err := newFacebook("", testToken); err == nil {
		t.Fatal("no page accepted")
	}
	if _, err := newFacebook("1234567890", ""); err == nil {
		t.Fatal("no token accepted")
	}
	if _, err := newFacebook("12 34/../x", testToken); err == nil {
		t.Fatal("odd page id accepted")
	}
}

func TestFacebookRunFollowsComments(t *testing.T) {
	f := &fbFake{live: true}
	fb := testFacebook(t, f)
	fb.pollEvery, fb.statsEvery = 10*time.Millisecond, 30*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { fb.run(ctx, out); close(done) }()
	waitFor(t, "three comments", func() bool { return out.s.view(10).total == 3 })
	waitFor(t, "viewers 15", func() bool { return out.last().Viewers == 15 })
	f.mu.Lock()
	first, second := f.since[0], f.since[1]
	f.live, f.ended = false, true
	f.mu.Unlock()
	// first poll: everything; then since the newest seen, minus a little
	want := time.Date(2026, 11, 21, 19, 44, 58, 0, time.UTC).Unix()
	if first != "" || second != itoa(want) {
		t.Fatalf("since %q then %q, want \"\" then %d", first, second, want)
	}
	waitFor(t, "back to waiting after the end", func() bool { return out.last().State == stWaiting })
	if v := out.s.view(10); ids(v.messages) != "facebook:LV1_c1 facebook:LV1_c2 facebook:LV1_c3 " {
		t.Fatalf("messages %q", ids(v.messages))
	}
	cancel()
	<-done
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
