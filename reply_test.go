package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// useTempTokens points the token file at a fresh folder for one test.
func useTempTokens(t *testing.T) *tokenStore {
	t.Helper()
	dir := t.TempDir()
	s := &tokenStore{path: filepath.Join(dir, "jadeck-addon-chat", "tokens.json")}
	tokensOnce.Lock()
	old := sharedTokens
	sharedTokens = s
	tokensOnce.Unlock()
	t.Cleanup(func() {
		tokensOnce.Lock()
		sharedTokens = old
		tokensOnce.Unlock()
	})
	return s
}

func TestTokenFileIsPrivateAndAtomic(t *testing.T) {
	s := useTempTokens(t)
	if err := s.put("twitch:yourchurch:abc", tokenEntry{Access: "acc-secret-1", Refresh: "ref-secret-1", Account: "Sam W."}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(s.path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", di.Mode().Perm())
	}
	// a second entry keeps the first; no temp files are left behind
	s.put("youtube:@yourchannel:cid", tokenEntry{Access: "acc-secret-2"})
	if e, ok := s.get("twitch:yourchurch:abc"); !ok || e.Refresh != "ref-secret-1" {
		t.Fatalf("first entry %+v %v", e, ok)
	}
	ents, _ := os.ReadDir(filepath.Dir(s.path))
	if len(ents) != 1 {
		t.Fatalf("folder has %d files", len(ents))
	}
	// concurrent writers don't lose entries
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); s.put(fmt.Sprintf("k%d", i), tokenEntry{Access: "a"}) }()
	}
	wg.Wait()
	for i := range 8 {
		if _, ok := s.get(fmt.Sprintf("k%d", i)); !ok {
			t.Fatalf("lost k%d", i)
		}
	}
	// sign out deletes the entry
	s.remove("twitch:yourchurch:abc")
	if _, ok := s.get("twitch:yourchurch:abc"); ok {
		t.Fatal("still signed in")
	}
	b, _ := os.ReadFile(s.path)
	if strings.Contains(string(b), "acc-secret-1") {
		t.Fatal("removed token still in the file")
	}
	if fi, _ := os.Stat(s.path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode after rewrite %v", fi.Mode().Perm())
	}
}

// --------------------------------------------------------------- Twitch

type twitchAPIFake struct {
	mu         sync.Mutex
	approved   bool
	refreshes  []string // refresh tokens presented
	valid      string   // the one refresh token that works now (single use)
	gen        int
	expired    bool // the next helix call says 401
	sent       []map[string]any
	authSeen   []string
	dropReason string
}

func (f *twitchAPIFake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/device", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_id") != "cid123" || r.Form.Get("scopes") != "user:write:chat" {
			t.Errorf("device form %v", r.Form)
		}
		if r.Form.Get("client_secret") != "" {
			t.Error("a public client sent a secret")
		}
		w.Write([]byte(`{"device_code":"dev-1","user_code":"KQ7M4TZP","verification_uri":"https://www.twitch.tv/activate?device-code=KQ7M4TZP","expires_in":1800,"interval":1}`))
	})
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if r.Form.Get("device_code") != "dev-1" {
				t.Errorf("device code %q", r.Form.Get("device_code"))
			}
			if !f.approved {
				w.WriteHeader(400)
				w.Write([]byte(`{"status":400,"message":"authorization_pending"}`))
				return
			}
			f.gen++
			f.valid = fmt.Sprintf("refresh-%d", f.gen)
			fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":%q,"expires_in":14400,"scope":["user:write:chat"],"token_type":"bearer"}`, f.gen, f.valid)
		case "refresh_token":
			rt := r.Form.Get("refresh_token")
			f.refreshes = append(f.refreshes, rt)
			if rt != f.valid || r.Form.Get("client_id") != "cid123" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"Bad Request","status":400,"message":"Invalid refresh token"}`))
				return
			}
			f.gen++
			f.valid = fmt.Sprintf("refresh-%d", f.gen) // single use: a new one each time
			fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":%q,"expires_in":14400}`, f.gen, f.valid)
		}
	})
	mux.HandleFunc("GET /helix/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "cid123" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("login") == "yourchurch" {
			w.Write([]byte(`{"data":[{"id":"1001","login":"yourchurch","display_name":"YourChurch"}]}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"2002","login":"sam_w","display_name":"Sam_W"}]}`))
	})
	mux.HandleFunc("POST /helix/chat/messages", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		auth := r.Header.Get("Authorization")
		f.authSeen = append(f.authSeen, auth)
		if f.expired {
			f.expired = false
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"Unauthorized","status":401,"message":"Invalid OAuth token"}`))
			return
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		f.sent = append(f.sent, b)
		if f.dropReason != "" {
			fmt.Fprintf(w, `{"data":[{"message_id":"","is_sent":false,"drop_reason":{"code":"msg_duplicate","message":%q}}]}`, f.dropReason)
			return
		}
		fmt.Fprintf(w, `{"data":[{"message_id":"m-%d","is_sent":true,"drop_reason":null}]}`, len(f.sent))
	})
	return mux
}

func TestTwitchDeviceSignInSendAndRefreshRotation(t *testing.T) {
	store := useTempTokens(t)
	f := &twitchAPIFake{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	tw, err := newTwitch("yourchurch", "cid123")
	if err != nil {
		t.Fatal(err)
	}
	tw.r.idBase, tw.r.helixBase, tw.r.hc = srv.URL+"/oauth2", srv.URL+"/helix", srv.Client()
	ctx := context.Background()

	if r := tw.signIn(ctx, signParams{Status: true}); r.OK || !strings.Contains(r.Text, "Not signed in") || r.Button != "Sign in to Twitch to reply" {
		t.Fatalf("status before %+v", r)
	}
	if ok, _, why := tw.canReply(); ok || why != "not signed in" {
		t.Fatalf("canReply before: %v %q", ok, why)
	}
	// press: a code to type, then pending
	r := tw.signIn(ctx, signParams{})
	if r.Code != "KQ7M4TZP" || !r.Poll || !strings.HasPrefix(r.URL, "https://www.twitch.tv/activate") || !strings.Contains(r.Text, "www.twitch.tv/activate") {
		t.Fatalf("start %+v", r)
	}
	tw.r.mu.Lock()
	tw.r.dev.next = time.Time{}
	tw.r.mu.Unlock()
	if r := tw.signIn(ctx, signParams{Poll: true}); !r.Poll || r.Code != "KQ7M4TZP" || r.OK {
		t.Fatalf("pending %+v", r)
	}
	// status while waiting shows the code again
	if r := tw.signIn(ctx, signParams{Status: true}); r.Code != "KQ7M4TZP" {
		t.Fatalf("status while waiting %+v", r)
	}
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
	tw.r.mu.Lock()
	tw.r.dev.next = time.Time{}
	tw.r.mu.Unlock()
	r = tw.signIn(ctx, signParams{Poll: true})
	if !r.OK || r.Text != "Can reply as Sam_W" || r.Button != "Sign out" || r.Poll {
		t.Fatalf("authorized %+v", r)
	}
	e, ok := store.get(tw.tokenKey())
	if !ok || e.Access != "access-1" || e.Refresh != "refresh-1" || e.UserID != "2002" || e.ChannelID != "1001" {
		t.Fatalf("saved %+v", e)
	}
	if ok, acct, _ := tw.canReply(); !ok || acct != "Sam_W" {
		t.Fatalf("canReply %v %q", ok, acct)
	}

	// a reply names the parent message
	id, err := tw.send(ctx, "Thanks!", &Message{ID: "twitch:abc-1", Platform: Twitch, Author: "Ruth"})
	if err != nil || id != "twitch:m-1" {
		t.Fatalf("send %q %v", id, err)
	}
	f.mu.Lock()
	b := f.sent[0]
	f.mu.Unlock()
	if b["broadcaster_id"] != "1001" || b["sender_id"] != "2002" || b["message"] != "Thanks!" || b["reply_parent_message_id"] != "abc-1" {
		t.Fatalf("body %v", b)
	}

	// the access token ran out: one refresh, the new refresh token saved
	store.update(tw.tokenKey(), func(x *tokenEntry) { x.Expiry = time.Now().Add(-time.Minute) })
	if _, err := tw.send(ctx, "Welcome!", nil); err != nil {
		t.Fatal(err)
	}
	e, _ = store.get(tw.tokenKey())
	if e.Access != "access-2" || e.Refresh != "refresh-2" {
		t.Fatalf("after refresh %+v", e)
	}
	// Twitch says 401 mid-life: renew once more with the NEW refresh token
	f.mu.Lock()
	f.expired = true
	f.mu.Unlock()
	if _, err := tw.send(ctx, "Again", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	refreshes := append([]string(nil), f.refreshes...)
	f.mu.Unlock()
	if strings.Join(refreshes, ",") != "refresh-1,refresh-2" {
		t.Fatalf("refresh tokens used %v (each once, the newest)", refreshes)
	}
	// a held-back message says why
	f.mu.Lock()
	f.dropReason = "Your message is identical to the one you sent within the last 30 seconds."
	f.mu.Unlock()
	if _, err := tw.send(ctx, "Again", nil); err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("drop: %v", err)
	}
	// a refused refresh signs out, and the error carries no token
	store.update(tw.tokenKey(), func(x *tokenEntry) { x.Expiry = time.Now().Add(-time.Minute); x.Refresh = "refresh-stale" })
	_, err = tw.send(ctx, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "sign in again") || strings.Contains(err.Error(), "refresh-stale") || strings.Contains(err.Error(), "access-") {
		t.Fatalf("stale refresh: %v", err)
	}
	if _, ok := store.get(tw.tokenKey()); ok {
		t.Fatal("still signed in after a refused refresh")
	}
}

func TestTwitchSignOut(t *testing.T) {
	store := useTempTokens(t)
	tw, _ := newTwitch("yourchurch", "cid123")
	store.put(tw.tokenKey(), tokenEntry{Access: "a-123456", Account: "Sam_W"})
	if r := tw.signIn(context.Background(), signParams{Status: true}); !r.OK || r.Button != "Sign out" {
		t.Fatalf("status %+v", r)
	}
	if r := tw.signIn(context.Background(), signParams{}); r.OK || !strings.Contains(r.Text, "Signed out") {
		t.Fatalf("sign out %+v", r)
	}
	if _, ok := store.get(tw.tokenKey()); ok {
		t.Fatal("token kept")
	}
	// no client id: the field says what to fill in
	tw2, _ := newTwitch("yourchurch", "")
	if r := tw2.signIn(context.Background(), signParams{Status: true}); !strings.Contains(r.Text, "Twitch app client ID") {
		t.Fatalf("no client id %+v", r)
	}
}

// -------------------------------------------------------------- YouTube

type googleFake struct {
	mu       sync.Mutex
	approved bool
	inserted []map[string]any
	auth     []string
}

func (f *googleFake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/device/code", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("scope") != youtubeScope || r.Form.Get("client_id") != "gcid" {
			t.Errorf("device form %v", r.Form)
		}
		w.Write([]byte(`{"device_code":"gdev","user_code":"ABCD-EFGH","verification_url":"https://www.google.com/device","expires_in":1800,"interval":5}`))
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Form.Get("client_secret") != "gsecret" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if !f.approved {
				w.WriteHeader(428)
				w.Write([]byte(`{"error":"authorization_pending","error_description":"Precondition Required"}`))
				return
			}
			w.Write([]byte(`{"access_token":"ya29.first","refresh_token":"1//grefresh","expires_in":3599,"scope":"https://www.googleapis.com/auth/youtube","token_type":"Bearer"}`))
		case "refresh_token":
			if r.Form.Get("refresh_token") != "1//grefresh" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
				return
			}
			w.Write([]byte(`{"access_token":"ya29.second","expires_in":3599}`))
		}
	})
	mux.HandleFunc("GET /yt/channels", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mine") != "true" || r.Header.Get("Authorization") != "Bearer ya29.first" {
			t.Errorf("channels %s %q", r.URL.RawQuery, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"items":[{"id":"UCxxxxxxxxxxxxxxxxxxxxxx","snippet":{"title":"Your Channel"}}]}`))
	})
	mux.HandleFunc("POST /yt/liveChat/messages", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Query().Get("part") != "snippet" || r.Header.Get("X-Goog-Api-Key") != "" {
			t.Errorf("insert %s (key header %q)", r.URL.RawQuery, r.Header.Get("X-Goog-Api-Key"))
		}
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		f.inserted = append(f.inserted, b)
		w.Write([]byte(`{"id":"LCC.new1","snippet":{"type":"textMessageEvent"}}`))
	})
	return mux
}

func TestYouTubeDeviceSignInAndInsert(t *testing.T) {
	store := useTempTokens(t)
	f := &googleFake{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	y, err := newYouTube(testKey, "@yourchannel", 0)
	if err != nil {
		t.Fatal(err)
	}
	y.base, y.hc, y.r.oauthBase = srv.URL+"/yt", srv.Client(), srv.URL+"/oauth"
	ctx := context.Background()
	if r := y.signIn(ctx, signParams{Status: true}); !strings.Contains(r.Text, "OAuth client ID") {
		t.Fatalf("no client %+v", r)
	}
	y.r.clientID, y.r.secret = "gcid", "gsecret"
	r := y.signIn(ctx, signParams{})
	if r.Code != "ABCD-EFGH" || r.URL != "https://www.google.com/device" || !r.Poll {
		t.Fatalf("start %+v", r)
	}
	y.r.mu.Lock()
	y.r.dev.next = time.Time{}
	y.r.mu.Unlock()
	if r := y.signIn(ctx, signParams{Poll: true}); !r.Poll || r.OK {
		t.Fatalf("pending %+v", r)
	}
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
	y.r.mu.Lock()
	y.r.dev.next = time.Time{}
	y.r.mu.Unlock()
	if r := y.signIn(ctx, signParams{Poll: true}); !r.OK || r.Text != "Can reply as Your Channel" {
		t.Fatalf("authorized %+v", r)
	}
	if e, ok := store.get(y.tokenKey()); !ok || e.Refresh != "1//grefresh" || e.Account != "Your Channel" {
		t.Fatalf("saved %+v", e)
	}
	// signed in, nothing live: can't reply yet
	if ok, acct, why := y.canReply(); ok || acct != "Your Channel" || why != "not live right now" {
		t.Fatalf("not live: %v %q %q", ok, acct, why)
	}
	y.setChat("chat-1")
	if ok, _, _ := y.canReply(); !ok {
		t.Fatal("live but can't reply")
	}
	id, err := y.send(ctx, "@Ruth K. Thank you!", nil)
	if err != nil || id != "youtube:LCC.new1" {
		t.Fatalf("send %q %v", id, err)
	}
	f.mu.Lock()
	b, _ := json.Marshal(f.inserted[0])
	auth := f.auth[0]
	f.mu.Unlock()
	if string(b) != `{"snippet":{"liveChatId":"chat-1","textMessageDetails":{"messageText":"@Ruth K. Thank you!"},"type":"textMessageEvent"}}` || auth != "Bearer ya29.first" {
		t.Fatalf("insert %s %q", b, auth)
	}
	// expired: refreshed with the secret, the refresh token kept
	store.update(y.tokenKey(), func(x *tokenEntry) { x.Expiry = time.Now().Add(-time.Hour) })
	if _, err := y.send(ctx, "Welcome", nil); err != nil {
		t.Fatal(err)
	}
	if e, _ := store.get(y.tokenKey()); e.Access != "ya29.second" || e.Refresh != "1//grefresh" {
		t.Fatalf("after refresh %+v", e)
	}
	// a revoked refresh (Testing mode's 7 days): signed out, plain words
	store.update(y.tokenKey(), func(x *tokenEntry) { x.Expiry = time.Now().Add(-time.Hour); x.Refresh = "1//old" })
	_, err = y.send(ctx, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "7 days") || strings.Contains(err.Error(), "1//old") || strings.Contains(err.Error(), "ya29") {
		t.Fatalf("revoked: %v", err)
	}
}

// ------------------------------------------------------------- Facebook

type fbPostFake struct {
	mu     sync.Mutex
	posts  []string // path + message
	noPerm bool
	scopes string
}

func (f *fbPostFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		r.ParseForm()
		if r.Form.Get("access_token") != testToken {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"bad token","code":190}}`))
			return
		}
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/me":
				w.Write([]byte(`{"id":"1234567890","name":"Example Page"}`))
			case "/debug_token":
				fmt.Fprintf(w, `{"data":{"is_valid":true,"scopes":[%s]}}`, f.scopes)
			default:
				w.WriteHeader(404)
			}
			return
		}
		if strings.Contains(r.URL.RawQuery, "access_token") {
			t.Error("token in a POST's address")
		}
		if f.noPerm {
			w.WriteHeader(403)
			fmt.Fprintf(w, `{"error":{"message":"(#200) Permissions error, token %s","type":"OAuthException","code":200}}`, testToken)
			return
		}
		f.posts = append(f.posts, r.URL.Path+" "+r.Form.Get("message"))
		w.Write([]byte(`{"id":"VID_c99"}`))
	})
}

func TestFacebookReplyVsEveryoneAndMissingPermission(t *testing.T) {
	f := &fbPostFake{scopes: `"pages_read_engagement","pages_manage_engagement"`}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	fb, _ := newFacebook("1234567890", testToken)
	fb.base, fb.hc = srv.URL, srv.Client()
	ctx := context.Background()

	if r := fb.signIn(ctx, signParams{Status: true}); !r.OK || r.Text != "Can reply as Example Page" {
		t.Fatalf("status %+v", r)
	}
	if ok, _, why := fb.canReply(); ok || why != "not live right now" {
		t.Fatalf("not live: %v %q", ok, why)
	}
	fb.setLive("LV1", "VID1")
	if ok, acct, _ := fb.canReply(); !ok || acct != "Example Page" {
		t.Fatalf("live: %v %q", ok, acct)
	}
	// to everyone: a comment on the live video's video
	if id, err := fb.send(ctx, "Welcome!", nil); err != nil || id != "facebook:VID_c99" {
		t.Fatalf("everyone %q %v", id, err)
	}
	// a reply: under that comment
	if _, err := fb.send(ctx, "¡Claro!", &Message{ID: "facebook:LV1_c1", Platform: Facebook, Author: "Ana L."}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	posts := strings.Join(f.posts, " | ")
	f.mu.Unlock()
	if posts != "/VID1/comments Welcome! | /LV1_c1/comments ¡Claro!" {
		t.Fatalf("posts %s", posts)
	}

	// the token lacks pages_manage_engagement: plain words, no token
	f.mu.Lock()
	f.noPerm = true
	f.mu.Unlock()
	_, err := fb.send(ctx, "x", nil)
	if err == nil || err.Error() != fbPermMissingText || strings.Contains(err.Error(), testToken) {
		t.Fatalf("no permission: %v", err)
	}
	if ok, _, why := fb.canReply(); ok || !strings.Contains(why, "pages_manage_engagement") {
		t.Fatalf("after a refusal: %v %q", ok, why)
	}
	// the status field says what's missing when debug_token shows it
	f.mu.Lock()
	f.scopes = `"pages_read_engagement"`
	f.mu.Unlock()
	if r := fb.signIn(ctx, signParams{Status: true}); r.OK || !strings.Contains(r.Error, "pages_manage_engagement") {
		t.Fatalf("status without the permission %+v", r)
	}
}

// -------------------------------------------------------------- Owncast

func TestOwncastSendsOverTheChatWebsocket(t *testing.T) {
	var mu sync.Mutex
	var got []string
	requireAuth := false
	var names []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat/register", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			DisplayName string `json:"displayName"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &b)
		mu.Lock()
		names = append(names, b.DisplayName)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"id": "me-1", "accessToken": "tok-abcdef", "displayName": b.DisplayName})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"online":true,"viewerCount":3}`))
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		p, err := wsAccept(w, r)
		if err != nil {
			return
		}
		defer p.c.Close()
		n := 0
		for {
			raw, err := p.read()
			if err != nil {
				return
			}
			var e struct{ Type, Body string }
			if json.Unmarshal([]byte(raw), &e) != nil || e.Type != "CHAT" {
				continue
			}
			mu.Lock()
			got = append(got, e.Body)
			auth := requireAuth
			mu.Unlock()
			if auth {
				p.send(`{"type":"CHAT_ACTION","id":"x","body":"Authentication is required to participate in chat."}`)
				continue
			}
			n++
			// everyone sees it, us too (our own user id)
			p.send(fmt.Sprintf(`{"type":"CHAT","id":"own-%d","timestamp":"2026-11-21T19:50:00Z","user":{"id":"me-1","displayName":"Booth"},"body":"<p>%s</p>"}`, n, e.Body))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oc, _ := newOwncast(srv.URL)
	oc.name = "Booth"
	owncastAckWait = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	out := newRecSink()
	done := make(chan struct{})
	go func() { oc.run(ctx, out); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "connected", func() bool { ok, _, _ := oc.canReply(); return ok })
	if _, acct, _ := oc.canReply(); acct != "Booth" {
		t.Fatalf("account %q", acct)
	}
	if r := oc.signIn(ctx, signParams{Status: true}); !r.OK || !strings.Contains(r.Text, `"Booth"`) {
		t.Fatalf("status %+v", r)
	}
	id, err := oc.send(ctx, shaped(Owncast, "Welcome!", &Message{Author: "Ruth"}), nil)
	if err != nil || id != "owncast:own-1" {
		t.Fatalf("send %q %v", id, err)
	}
	mu.Lock()
	if len(names) == 0 || names[0] != "Booth" || len(got) != 1 || got[0] != "@Ruth Welcome!" {
		t.Fatalf("register %v sent %v", names, got)
	}
	requireAuth = true
	mu.Unlock()
	// our own message isn't shown again by the feed (the hub has it as YOU)
	if _, ok := out.s.get("owncast:own-1"); ok {
		t.Fatal("own echo shown")
	}
	_, err = oc.send(ctx, "again", nil)
	if err == nil || !strings.Contains(err.Error(), "signed-in users") {
		t.Fatalf("auth required: %v", err)
	}
	if ok, _, why := oc.canReply(); ok || !strings.Contains(why, "signed-in") {
		t.Fatalf("after refusal %v %q", ok, why)
	}
}

// ----------------------------------------------------------------- hub

// replSource is a platform source with a replier the test drives.
type replSource struct {
	*ctlSource
	mu      sync.Mutex
	ok      bool
	account string
	fail    error
	sent    []string
	n       int
}

func (s *replSource) canReply() (bool, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ok {
		return false, "", "not signed in"
	}
	return true, s.account, ""
}

func (s *replSource) send(_ context.Context, text string, replyTo *Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return "", s.fail
	}
	s.n++
	s.sent = append(s.sent, text)
	return fmt.Sprintf("%s:echo-%d", s.plat, s.n), nil
}

func (s *replSource) sends() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

func TestHubSendPartialFailureRetryAndEchoes(t *testing.T) {
	useTempTokens(t)
	old := sendEvery
	sendEvery = 0
	defer func() { sendEvery = old }()

	yt := &replSource{ctlSource: newCtl(YouTube), ok: true, account: "Your Channel"}
	tw := &replSource{ctlSource: newCtl(Twitch), ok: true, account: "YourChurch"}
	fb := &replSource{ctlSource: newCtl(Facebook), ok: true, account: "Example Page", fail: errors.New(fbPermMissingText)}
	rb := newCtl(Rumble)
	ps := []*platformConn{openPlatform(yt, "@yourchannel"), openPlatform(tw, "yourchurch"), openPlatform(fb, "1234567890"), openPlatform(rb, "")}
	defer func() {
		for _, p := range ps {
			p.Close()
		}
	}()
	hub, err := Open(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	ctx := context.Background()

	st := hubState(t, hub)
	srcs := st["sources"].([]wireSource)
	if len(srcs) != 4 || !srcs[0].CanReply || srcs[0].ReplyAs != "Your Channel" || srcs[3].CanReply || srcs[3].ReplyWhy != "read only" || st["canSend"] != true {
		t.Fatalf("sources %+v", srcs)
	}

	if _, err := hub.AddonAction(ctx, "send_message", json.RawMessage(`{"text":"   "}`)); err == nil {
		t.Fatal("sent an empty message")
	}
	res, err := hub.AddonAction(ctx, "send_message", json.RawMessage(`{"text":" Welcome! Today's verse: John 3:16 "}`))
	if err != nil {
		t.Fatal(err)
	}
	r := res.(map[string]any)
	if r["sent"] != 2 || r["of"] != 3 || r["ok"] != false {
		t.Fatalf("result %v", r)
	}
	sendID := r["id"].(string)
	st = hubState(t, hub)
	last := st["lastSend"].(map[string]any)
	results := last["results"].([]map[string]any)
	var fbErr string
	for _, x := range results {
		if x["platform"] == Facebook {
			fbErr, _ = x["error"].(string)
		}
	}
	if !strings.HasPrefix(fbErr, "Facebook: the Page token can't post yet") || last["sent"] != 2 {
		t.Fatalf("last send %v", last)
	}
	// YOU is in the chat at once, with ✓ / ✕
	var mine *wireMessage
	for _, m := range st["messages"].([]wireMessage) {
		if m.Mine {
			mine = &m
		}
	}
	if mine == nil || mine.Author != "Your Channel" || mine.Text != "Welcome! Today's verse: John 3:16" || len(mine.Sent) != 3 || mine.Sent[2].OK {
		t.Fatalf("mine %+v", mine)
	}

	// the platforms echo it: by id (YouTube) and by account + text (Twitch, another id)
	yt.in <- Message{ID: "youtube:echo-1", Platform: YouTube, Author: "Your Channel", Text: "Welcome! Today's verse: John 3:16", At: time.Now()}
	tw.in <- Message{ID: "twitch:other-id", Platform: Twitch, Author: "YourChurch", Text: "Welcome!  Today's verse: John 3:16", At: time.Now()}
	tw.in <- Message{ID: "twitch:x1", Platform: Twitch, Author: "Ruth", Text: "Amen", At: time.Now()}
	waitFor(t, "Ruth's message", func() bool { _, ok := hub.store.get("twitch:x1"); return ok })
	n := 0
	for _, m := range hubState(t, hub)["messages"].([]wireMessage) {
		if strings.Contains(m.Text, "John 3:16") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the sent message shows %d times", n)
	}

	// retry: only Facebook again
	fb.mu.Lock()
	fb.fail = nil
	fb.mu.Unlock()
	res, err = hub.AddonAction(ctx, "retry_send", json.RawMessage(`{"sendId":"`+sendID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(map[string]any); r["sent"] != 3 || r["ok"] != true {
		t.Fatalf("retry %v", r)
	}
	if got := strings.Join(yt.sends(), "|") + "/" + strings.Join(tw.sends(), "|") + "/" + strings.Join(fb.sends(), "|"); got != "Welcome! Today's verse: John 3:16/Welcome! Today's verse: John 3:16/Welcome! Today's verse: John 3:16" {
		t.Fatalf("sent %s", got)
	}
	m, _ := hub.store.get("you:" + sendID)
	if len(m.Sent) != 3 || !m.Sent[2].OK {
		t.Fatalf("marks after retry %+v", m.Sent)
	}

	// a reply goes to its platform only, "@author" where there are no threads
	ruth := Message{ID: "youtube:r1", Platform: YouTube, Author: "Ruth K.", Text: "Hello", At: time.Now()}
	yt.in <- ruth
	waitFor(t, "Ruth on YouTube", func() bool { _, ok := hub.store.get("youtube:r1"); return ok })
	res, err = hub.AddonAction(ctx, "send_message", json.RawMessage(`{"text":"Welcome!","replyTo":"youtube:r1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(map[string]any); r["of"] != 1 || r["sent"] != 1 {
		t.Fatalf("reply %v", r)
	}
	if s := yt.sends(); s[len(s)-1] != "@Ruth K. Welcome!" || len(tw.sends()) != 1 {
		t.Fatalf("reply sends %v / %v", s, tw.sends())
	}
	// too long for YouTube: refused there only, in plain words
	long := strings.Repeat("a", 201)
	res, _ = hub.AddonAction(ctx, "send_message", json.RawMessage(`{"text":"`+long+`","platforms":"youtube,twitch"}`))
	r = res.(map[string]any)
	if r["sent"] != 1 || r["of"] != 2 || !strings.Contains(fmt.Sprint(r["results"]), "at most 200 characters") {
		t.Fatalf("long %v", r)
	}
	// a platform that can't reply (signed out) isn't picked by default
	tw.mu.Lock()
	tw.ok = false
	tw.mu.Unlock()
	res, _ = hub.AddonAction(ctx, "send_message", json.RawMessage(`{"text":"Bye"}`))
	if r := res.(map[string]any); r["of"] != 2 {
		t.Fatalf("default targets %v", r)
	}
	// dismiss
	hub.AddonAction(ctx, "dismiss_send", nil)
	if hubState(t, hub)["lastSend"] != nil {
		t.Fatal("result card still there")
	}
}

// No token ever reaches the widget's state or the action results.
func TestNoTokenInStateOrErrors(t *testing.T) {
	store := useTempTokens(t)
	tw, _ := newTwitch("yourchurch", "cid123")
	tw.r.helixBase, tw.r.idBase, tw.url = "http://127.0.0.1:1/helix", "http://127.0.0.1:1/oauth2", "ws://127.0.0.1:1"
	store.put(tw.tokenKey(), tokenEntry{Access: "secret-access-777", Refresh: "secret-refresh-888", Account: "Sam_W", Expiry: time.Now().Add(time.Hour)})
	p := openPlatform(tw, tw.channel)
	defer p.Close()
	hub, _ := Open(json.RawMessage(`{}`))
	defer hub.Close()
	res, _ := hub.AddonAction(context.Background(), "send_message", json.RawMessage(`{"text":"hi","platforms":"twitch"}`))
	st := hubState(t, hub)
	b, _ := json.Marshal(map[string]any{"state": st, "res": res, "signin": p.f.rep.(signer).signIn(context.Background(), signParams{Status: true})})
	if s := string(b); strings.Contains(s, "secret-access") || strings.Contains(s, "secret-refresh") {
		t.Fatalf("token leaked: %s", s)
	}
	if !strings.Contains(string(b), "Sam_W") {
		t.Fatalf("no account name: %s", b)
	}
}
