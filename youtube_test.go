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

const testKey = "AIzaTEST-key-0123456789"

func TestParseYouTubeTarget(t *testing.T) {
	for in, want := range map[string]string{
		"UCabcdefghijklmnopqrstuv": "channel UCabcdefghijklmnopqrstuv",
		"@SomeChannel":             "handle @SomeChannel",
		"dQw4w9WgXcQ":              "video dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1s":         "video dQw4w9WgXcQ",
		"youtu.be/dQw4w9WgXcQ":                                     "video dQw4w9WgXcQ",
		"https://youtube.com/live/dQw4w9WgXcQ?si=x":                "video dQw4w9WgXcQ",
		"https://www.youtube.com/channel/UCabcdefghijklmnopqrstuv": "channel UCabcdefghijklmnopqrstuv",
		"https://www.youtube.com/@SomeChannel/streams":             "handle @SomeChannel",
		" studio.youtube.com/video/dQw4w9WgXcQ/livestreaming ":     "video dQw4w9WgXcQ",
	} {
		k, id, err := parseYouTubeTarget(in)
		if err != nil || k+" "+id != want {
			t.Errorf("%q → %s %s %v, want %s", in, k, id, err, want)
		}
	}
	for _, bad := range []string{"", "hello world", "https://vimeo.com/123", "UCshort"} {
		if _, _, err := parseYouTubeTarget(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// ytFake is a fake YouTube Data API.
type ytFake struct {
	mu       sync.Mutex
	calls    []string
	live     bool   // the newest upload is live
	chatErr  string // reason to fail liveChat/messages with
	pages    int
	searched int
	chatAt   []time.Time
}

func (f *ytFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != testKey {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","errors":[{"reason":"keyInvalid"}]}}`))
			return
		}
		if strings.Contains(r.URL.RawQuery, testKey) {
			t.Errorf("API key in the URL: %s", r.URL)
		}
		q := r.URL.Query()
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Path)
		live, chatErr := f.live, f.chatErr
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/channels":
			if q.Get("forHandle") != "@SomeChannel" && q.Get("id") != "UCabcdefghijklmnopqrstuv" {
				w.Write([]byte(`{"items":[]}`))
				return
			}
			w.Write([]byte(`{"items":[{"id":"UCabcdefghijklmnopqrstuv","contentDetails":{"relatedPlaylists":{"uploads":"UUabcdefghijklmnopqrstuv"}}}]}`))
		case "/playlistItems":
			if q.Get("playlistId") != "UUabcdefghijklmnopqrstuv" {
				t.Errorf("playlist %s", q.Get("playlistId"))
			}
			w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"liveVideo01"}},{"contentDetails":{"videoId":"oldVideo001"}}]}`))
		case "/search":
			f.mu.Lock()
			f.searched++
			f.mu.Unlock()
			if q.Get("eventType") != "live" || q.Get("channelId") != "UCabcdefghijklmnopqrstuv" {
				t.Errorf("search %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"items":[]}`))
		case "/videos":
			var items []string
			for _, id := range strings.Split(q.Get("id"), ",") {
				if id == "liveVideo01" && live {
					items = append(items, `{"id":"liveVideo01","snippet":{"title":"Sunday stream","liveBroadcastContent":"live"},
						"liveStreamingDetails":{"actualStartTime":"2026-11-21T18:50:00Z","concurrentViewers":"21","activeLiveChatId":"CHAT1"}}`)
				} else {
					items = append(items, `{"id":"`+id+`","snippet":{"title":"old","liveBroadcastContent":"none"},
						"liveStreamingDetails":{"actualStartTime":"2026-11-14T18:50:00Z","actualEndTime":"2026-11-14T20:50:00Z"}}`)
				}
			}
			w.Write([]byte(`{"items":[` + strings.Join(items, ",") + `]}`))
		case "/liveChat/messages":
			if chatErr != "" {
				w.WriteHeader(403)
				w.Write([]byte(`{"error":{"code":403,"message":"nope","errors":[{"reason":"` + chatErr + `"}]}}`))
				return
			}
			if q.Get("liveChatId") != "CHAT1" || q.Get("part") != "snippet,authorDetails" {
				t.Errorf("chat query %s", r.URL.RawQuery)
			}
			f.mu.Lock()
			f.pages++
			f.chatAt = append(f.chatAt, time.Now())
			n := f.pages
			f.mu.Unlock()
			if n == 1 {
				if q.Get("pageToken") != "" {
					t.Errorf("first page with a token")
				}
				w.Write([]byte(ytPage1))
				return
			}
			if q.Get("pageToken") != "NEXT1" {
				t.Errorf("page token %q", q.Get("pageToken"))
			}
			w.Write([]byte(`{"pollingIntervalMillis":10,"nextPageToken":"NEXT2","items":[
				{"id":"m3","snippet":{"type":"messageDeletedEvent","publishedAt":"2026-11-21T19:44:30Z","messageDeletedDetails":{"deletedMessageId":"m1"}},"authorDetails":{}}]}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

const ytPage1 = `{"pollingIntervalMillis":300,"nextPageToken":"NEXT1","items":[
 {"id":"m1","snippet":{"type":"textMessageEvent","publishedAt":"2026-11-21T19:42:01.123Z","displayMessage":"Hello!","textMessageDetails":{"messageText":"Hello!"}},
  "authorDetails":{"displayName":"Ruth K.","profileImageUrl":"https://yt3.example/ruth.jpg"}},
 {"id":"m2","snippet":{"type":"superChatEvent","publishedAt":"2026-11-21T19:44:00Z","displayMessage":"$10.00 from Pedro M.: For the fund",
  "superChatDetails":{"amountDisplayString":"$10.00","userComment":"For the fund"}},"authorDetails":{"displayName":"Pedro M."}},
 {"id":"m4","snippet":{"type":"superStickerEvent","publishedAt":"2026-11-21T19:45:00Z","superStickerDetails":{"amountDisplayString":"$2.00"}},"authorDetails":{"displayName":"Lily S."}},
 {"id":"m5","snippet":{"type":"pollEvent","publishedAt":"2026-11-21T19:45:00Z"},"authorDetails":{}}
]}`

func TestParseYouTubeChat(t *testing.T) {
	var r ytChatResponse
	if err := json.Unmarshal([]byte(ytPage1), &r); err != nil {
		t.Fatal(err)
	}
	p := parseYouTubeChat(r)
	if p.next != "NEXT1" || p.interval != 300*time.Millisecond || p.ended || len(p.msgs) != 3 {
		t.Fatalf("page %+v", p)
	}
	m := p.msgs[0]
	if m.ID != "youtube:m1" || m.Author != "Ruth K." || m.Text != "Hello!" || m.Avatar != "https://yt3.example/ruth.jpg" ||
		!m.At.Equal(time.Date(2026, 11, 21, 19, 42, 1, 123e6, time.UTC)) {
		t.Fatalf("text message %+v", m)
	}
	if sc := p.msgs[1]; sc.Super != "$10.00" || sc.Text != "For the fund" || sc.Author != "Pedro M." {
		t.Fatalf("super chat %+v", sc)
	}
	if st := p.msgs[2]; st.Super != "$2.00" || st.Text != "Super Sticker" {
		t.Fatalf("super sticker %+v", st)
	}
	r = ytChatResponse{OfflineAt: "2026-11-21T21:00:00Z"}
	if !parseYouTubeChat(r).ended {
		t.Fatal("offlineAt didn't end the chat")
	}
}

func testYouTube(t *testing.T, f *ytFake, target string) *youtube {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	y, err := newYouTube(testKey, target, 0)
	if err != nil {
		t.Fatal(err)
	}
	y.base, y.hc = srv.URL, srv.Client()
	return y
}

func TestYouTubeFindsTheChannelsLiveStream(t *testing.T) {
	f := &ytFake{live: true}
	y := testYouTube(t, f, "@SomeChannel")
	lv, err := y.findLive(context.Background())
	if err != nil || lv.chatID != "CHAT1" || lv.videoID != "liveVideo01" || lv.viewers != 21 || lv.title != "Sunday stream" {
		t.Fatalf("live %+v %v", lv, err)
	}
	if f.searched != 0 {
		t.Fatal("used search.list (100 units) although the uploads had it")
	}
	// nothing live: one search fallback, then not again for a while
	f.live = false
	for i := 0; i < 3; i++ {
		if _, err := y.findLive(context.Background()); err != errNotLive {
			t.Fatalf("not live: %v", err)
		}
	}
	if f.searched != 1 {
		t.Fatalf("searched %d times, want 1", f.searched)
	}
	// the channel is looked up once
	n := 0
	for _, c := range f.calls {
		if c == "/channels" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("channels.list %d times", n)
	}
}

func TestYouTubeVideoTargetAndBadKey(t *testing.T) {
	f := &ytFake{live: true}
	y := testYouTube(t, f, "https://youtu.be/liveVideo01")
	if lv, err := y.findLive(context.Background()); err != nil || lv.chatID != "CHAT1" {
		t.Fatalf("%+v %v", lv, err)
	}
	y.key = "wrong-key-123456"
	_, err := y.findLive(context.Background())
	if ytReason(err) != "keyInvalid" || y.problem(err) != "YouTube didn't accept the API key" {
		t.Fatalf("bad key: %v / %s", err, y.problem(err))
	}
	if strings.Contains(err.Error(), "wrong-key") {
		t.Fatal("key in the error")
	}
}

func TestYouTubePollChatEndedAndQuota(t *testing.T) {
	f := &ytFake{live: true, chatErr: "liveChatEnded"}
	y := testYouTube(t, f, "liveVideo01")
	if p, err := y.pollChat(context.Background(), "CHAT1", ""); err != nil || !p.ended {
		t.Fatalf("ended: %+v %v", p, err)
	}
	f.chatErr = "quotaExceeded"
	_, err := y.pollChat(context.Background(), "CHAT1", "")
	if !strings.Contains(y.problem(err), "quota") {
		t.Fatalf("quota: %v", err)
	}
}

// recSink records what a source sends.
type recSink struct {
	mu       sync.Mutex
	s        *store
	statuses []sourceStatus
}

func newRecSink() *recSink          { return &recSink{s: newStore()} }
func (r *recSink) add(m ...Message) { r.s.add(m...) }
func (r *recSink) remove(id string) { r.s.remove(id) }
func (r *recSink) status(st sourceStatus) {
	r.mu.Lock()
	r.statuses = append(r.statuses, st)
	r.mu.Unlock()
}
func (r *recSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.statuses)
}

func (r *recSink) last() sourceStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.statuses) == 0 {
		return sourceStatus{}
	}
	return r.statuses[len(r.statuses)-1]
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestYouTubeRunFollowsTheChat(t *testing.T) {
	f := &ytFake{live: true}
	y := testYouTube(t, f, "UCabcdefghijklmnopqrstuv")
	y.minPoll = 20 * time.Millisecond // below the server's 300 ms: the server's interval wins
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { y.run(ctx, out); close(done) }()
	waitFor(t, "the delete on page 2", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.pages >= 2
	})
	waitFor(t, "m1 removed", func() bool { _, ok := out.s.get("youtube:m1"); return !ok })
	if st := out.last(); st.State != stLive || st.Viewers != 21 {
		t.Fatalf("status %+v", st)
	}
	if v := out.s.view(10); ids(v.messages) != "youtube:m2 youtube:m4 " {
		t.Fatalf("messages %q", ids(v.messages))
	}
	// the first page asked for 300 ms: page 2 must not come early
	f.mu.Lock()
	gap := f.chatAt[1].Sub(f.chatAt[0])
	f.mu.Unlock()
	if gap < 290*time.Millisecond {
		t.Fatalf("polled again after %v; YouTube asked for 300ms", gap)
	}
	cancel()
	<-done
}
