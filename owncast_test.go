package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOwncastBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://live.example.org/":       "https://live.example.org",
		"http://192.168.1.50:8080":        "http://192.168.1.50:8080",
		"192.168.1.50:8080":               "http://192.168.1.50:8080",
		"localhost:8080":                  "http://localhost:8080",
		"live.example.org":                "https://live.example.org",
		"https://example.org/owncast?x=1": "https://example.org/owncast",
	} {
		if got, err := owncastBase(in); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://"} {
		if _, err := owncastBase(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseOwncast(t *testing.T) {
	raw := `{"type":"CHAT","id":"c1","timestamp":"2026-11-21T19:41:00.123Z","user":{"displayName":"Ruth"},"body":"<p>Good &amp; morning <img class=\"emoji\" alt=\":pray:\" src=\"/img/emoji/pray.gif\"></p>","visible":true}
{"type":"CHAT_ACTION","id":"a1","body":"Sam joined the chat"}
{"type":"SYSTEM","id":"s1","timestamp":"2026-11-21T19:42:00Z","body":"Welcome!"}
{"type":"CHAT","id":"c2","user":{"displayName":"Hidden"},"body":"x","visible":false}
{"type":"VISIBILITY-UPDATE","ids":["c0"],"visible":false}`
	msgs, hidden := parseOwncast(raw)
	if len(msgs) != 2 || msgs[0].ID != "owncast:c1" || msgs[0].Text != "Good & morning :pray:" || msgs[0].Author != "Ruth" ||
		msgs[0].At.Format(time.RFC3339Nano) != "2026-11-21T19:41:00.123Z" {
		t.Fatalf("messages %+v", msgs)
	}
	if msgs[1].Author != "Owncast" || msgs[1].Text != "Welcome!" {
		t.Fatalf("system %+v", msgs[1])
	}
	if len(hidden) != 1 || hidden[0] != "owncast:c0" {
		t.Fatalf("hidden %v", hidden)
	}
}

// owncastFake is an Owncast server: register, status and the chat websocket.
type owncastFake struct {
	mu        sync.Mutex
	online    bool
	refuse    bool
	names     []string
	tokens    []string
	statusN   int
	wsN       int
	dropFirst bool // close the first websocket after one message
}

func (f *owncastFake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.refuse {
			http.Error(w, "chat is disabled", 403)
			return
		}
		var b struct {
			DisplayName string `json:"displayName"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &b)
		f.names = append(f.names, b.DisplayName)
		json.NewEncoder(w).Encode(map[string]string{"id": "u1", "accessToken": "tok-123456", "displayName": b.DisplayName})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.statusN++
		json.NewEncoder(w).Encode(map[string]any{"online": f.online, "viewerCount": 5, "streamTitle": "Sabbath service"})
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokens = append(f.tokens, r.URL.Query().Get("accessToken"))
		f.wsN++
		n, drop := f.wsN, f.dropFirst
		f.mu.Unlock()
		p, err := wsAccept(w, r)
		if err != nil {
			return
		}
		defer p.close()
		if n == 1 {
			// two events in one frame, one per line
			p.send(`{"type":"CHAT","id":"c1","timestamp":"2026-11-21T19:41:00Z","user":{"displayName":"Ruth"},"body":"<p>Hello from <b>Phoenix</b></p>"}` + "\n" +
				`{"type":"CHAT","id":"c2","timestamp":"2026-11-21T19:42:00Z","user":{"displayName":"Spam"},"body":"spam"}`)
			if drop {
				return
			}
			p.send(`{"type":"VISIBILITY-UPDATE","ids":["c2"],"visible":false}`)
		} else {
			p.send(`{"type":"CHAT","id":"c3","timestamp":"2026-11-21T19:43:00Z","user":{"displayName":"Ana"},"body":"back"}`)
		}
		p.read()
	})
	return mux
}

func testOwncast(t *testing.T, f *owncastFake) *owncast {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	oc, err := newOwncast(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	oc.statusEvery, oc.minBackoff, oc.maxBackoff = 20*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond
	return oc
}

func TestOwncastReadsChatAndStatus(t *testing.T) {
	f := &owncastFake{}
	oc := testOwncast(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { oc.run(ctx, out); close(done) }()
	waitFor(t, "c2 hidden", func() bool { _, ok := out.s.get("owncast:c1"); _, spam := out.s.get("owncast:c2"); return ok && !spam })
	if st := out.last(); st.State != stWaiting || !strings.Contains(st.Note, "waiting for the stream") {
		t.Fatalf("offline status %+v", st)
	}
	f.mu.Lock()
	f.online = true
	f.mu.Unlock()
	waitFor(t, "live", func() bool { return out.last().State == stLive })
	if st := out.last(); st.Viewers != 5 || st.Title != "Sabbath service" {
		t.Fatalf("live status %+v", st)
	}
	f.mu.Lock()
	names, tokens := f.names, f.tokens
	f.mu.Unlock()
	if names[0] != "jadeck" || tokens[0] != "tok-123456" {
		t.Fatalf("register %v tokens %v", names, tokens)
	}
	m, _ := out.s.get("owncast:c1")
	if m.Text != "Hello from Phoenix" || m.Author != "Ruth" {
		t.Fatalf("message %+v", m)
	}
	cancel()
	<-done
}

func TestOwncastReconnectsAndReportsARefusedLogin(t *testing.T) {
	f := &owncastFake{dropFirst: true}
	oc := testOwncast(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	out := newRecSink()
	done := make(chan struct{})
	go func() { oc.run(ctx, out); close(done) }()
	waitFor(t, "c3 after the reconnect", func() bool { _, ok := out.s.get("owncast:c3"); return ok })
	cancel()
	<-done

	f = &owncastFake{refuse: true}
	oc = testOwncast(t, f)
	ctx, cancel = context.WithCancel(context.Background())
	out = newRecSink()
	done = make(chan struct{})
	go func() { oc.run(ctx, out); close(done) }()
	waitFor(t, "the error", func() bool { return out.last().State == stError })
	if n := out.last().Note; !strings.Contains(n, "didn't accept an anonymous chat login") {
		t.Fatalf("note %q", n)
	}
	cancel()
	<-done
}

// A server behind a CDN (like Owncast's own demo) names another host for
// the websocket in /api/config: the chat must connect there.
func TestOwncastSocketHostOverride(t *testing.T) {
	sock := &owncastFake{}
	sockSrv := httptest.NewServer(sock.handler(t))
	t.Cleanup(sockSrv.Close)
	front := http.NewServeMux()
	front.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"socketHostOverride": sockSrv.URL, "chatDisabled": false})
	})
	front.Handle("/", sock.handler(t)) // register and status answer here too
	front.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no upgrade through the CDN", 400) })
	frontSrv := httptest.NewServer(front)
	t.Cleanup(frontSrv.Close)
	oc, err := newOwncast(frontSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	oc.statusEvery, oc.minBackoff, oc.maxBackoff = 20*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { oc.run(ctx, out); close(done) }()
	waitFor(t, "chat through the override host", func() bool { _, ok := out.s.get("owncast:c1"); return ok })
	cancel()
	<-done
}

func TestOwncastChatDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"chatDisabled": true})
	})
	mux.Handle("/", (&owncastFake{}).handler(t))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oc, _ := newOwncast(srv.URL)
	oc.minBackoff, oc.maxBackoff = 10*time.Millisecond, 40*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	go oc.run(ctx, out)
	waitFor(t, "chat off reported", func() bool { return strings.Contains(out.last().Note, "chat is turned off") })
}
