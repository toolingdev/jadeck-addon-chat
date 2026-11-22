package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseTwitchChannel(t *testing.T) {
	for in, want := range map[string]string{
		"YourChurch": "yourchurch", "#yourchurch": "yourchurch", "@your_church": "your_church",
		"https://www.twitch.tv/YourChurch": "yourchurch", "twitch.tv/yourchurch/videos": "yourchurch",
		"https://m.twitch.tv/yourchurch?x=1": "yourchurch", "https://www.twitch.tv/popout/yourchurch/chat": "yourchurch",
	} {
		if got, err := parseTwitchChannel(in); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ab", "has space", "a-dash", strings.Repeat("x", 26), "https://example.com/yourchurch"} {
		if _, err := parseTwitchChannel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseIRC(t *testing.T) {
	m := parseIRC(`@badge-info=;bits=100;display-name=Ruth\sK.;id=abc-1;tmi-sent-ts=1700000000000;user-id=42 :ruth!ruth@ruth.tmi.twitch.tv PRIVMSG #yourchurch :Cheer100 Amen: blessings`)
	if m.command != "PRIVMSG" || m.tags["display-name"] != "Ruth K." || m.trailing() != "Cheer100 Amen: blessings" || m.params[0] != "#yourchurch" {
		t.Fatalf("%+v", m)
	}
	msg, ok := twitchMessage(m)
	if !ok || msg.ID != "twitch:abc-1" || msg.Super != "100 bits" || msg.Author != "Ruth K." || !msg.At.Equal(time.UnixMilli(1700000000000)) {
		t.Fatalf("%+v", msg)
	}
	if m := parseIRC("PING :tmi.twitch.tv"); m.command != "PING" || m.trailing() != "tmi.twitch.tv" {
		t.Fatalf("ping %+v", m)
	}
	if got := unescapeTag(`a\sb\:c\\d\n`); got != "a b;c\\d\n" {
		t.Fatalf("unescape %q", got)
	}
	// /me
	msg, _ = twitchMessage(parseIRC("@id=x1;display-name=Sam :sam!sam@sam PRIVMSG #c :\x01ACTION waves\x01"))
	if msg.Text != "waves" {
		t.Fatalf("action %q", msg.Text)
	}
	// raids and the like aren't chat
	if _, ok := twitchMessage(parseIRC("@id=r1;msg-id=raid;display-name=X :tmi.twitch.tv USERNOTICE #c")); ok {
		t.Fatal("raid became a message")
	}
}

// twitchFake is a Twitch IRC-over-websocket server.
type twitchFake struct {
	mu       sync.Mutex
	sessions int
	login    []string // the first session's login lines
	pong     string
	notice   string // a NOTICE msg-id to answer JOIN with
	t        *testing.T
}

func (f *twitchFake) handler(w http.ResponseWriter, r *http.Request) {
	p, err := wsAccept(w, r)
	if err != nil {
		return
	}
	defer p.close()
	f.mu.Lock()
	f.sessions++
	n := f.sessions
	f.mu.Unlock()
	var nick string
	for i := 0; i < 4; i++ {
		line, err := p.read()
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\r\n")
		if n == 1 {
			f.mu.Lock()
			f.login = append(f.login, line)
			f.mu.Unlock()
		}
		if s, ok := strings.CutPrefix(line, "NICK "); ok {
			nick = s
		}
	}
	if f.notice != "" {
		p.send("@msg-id=" + f.notice + " :tmi.twitch.tv NOTICE #yourchurch :This channel has been suspended.\r\n")
		p.read()
		return
	}
	p.send(":tmi.twitch.tv CAP * ACK :twitch.tv/tags twitch.tv/commands\r\n:tmi.twitch.tv 001 " + nick + " :Welcome, GLHF!\r\n")
	p.send(":" + nick + "!" + nick + "@" + nick + ".tmi.twitch.tv JOIN #yourchurch\r\n@room-id=1 :tmi.twitch.tv ROOMSTATE #yourchurch\r\n")
	if n > 1 {
		p.send("@id=after;display-name=Back;tmi-sent-ts=1700000100000;user-id=9 :back!back@back PRIVMSG #yourchurch :back again\r\n")
		p.read() // until the client goes
		return
	}
	// two lines in one frame, then a cheer
	p.send("@id=m1;display-name=Ruth;tmi-sent-ts=1700000001000;user-id=1 :ruth!ruth@ruth PRIVMSG #yourchurch :Good morning\r\n" +
		"@id=m2;display-name=Spam;tmi-sent-ts=1700000002000;user-id=2 :spam!spam@spam PRIVMSG #yourchurch :buy followers\r\n")
	p.send("@id=m3;bits=50;display-name=Sam;tmi-sent-ts=1700000003000;user-id=3 :sam!sam@sam PRIVMSG #yourchurch :cheer50 amen\r\n")
	p.send("@id=m4;msg-id=resub;display-name=Ana;login=ana;system-msg=Ana\\ssubscribed\\sfor\\s3\\smonths!;tmi-sent-ts=1700000004000;user-id=4 :tmi.twitch.tv USERNOTICE #yourchurch :Three months!\r\n")
	p.send("@id=m5;display-name=Spam;tmi-sent-ts=1700000005000;user-id=2 :spam!spam@spam PRIVMSG #yourchurch :more spam\r\n")
	p.send("@id=m6;display-name=Oops;tmi-sent-ts=1700000006000;user-id=6 :oops!oops@oops PRIVMSG #yourchurch :typo\r\n")
	p.send("PING :tmi.twitch.tv\r\n")
	if pong, _ := p.read(); true {
		f.mu.Lock()
		f.pong = pong
		f.mu.Unlock()
	}
	p.send("@login=oops;target-msg-id=m6 :tmi.twitch.tv CLEARMSG #yourchurch :typo\r\n")
	p.send("@ban-duration=600;target-user-id=2 :tmi.twitch.tv CLEARCHAT #yourchurch :spam\r\n")
	p.send(":tmi.twitch.tv CLEARCHAT #yourchurch\r\n") // the whole chat: kept here
	p.send(":tmi.twitch.tv RECONNECT\r\n")
	p.read()
}

func testTwitch(t *testing.T, f *twitchFake) *twitch {
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	tw, err := newTwitch("YourChurch")
	if err != nil {
		t.Fatal(err)
	}
	tw.url = "ws" + strings.TrimPrefix(srv.URL, "http")
	tw.minBackoff, tw.maxBackoff = 10*time.Millisecond, 40*time.Millisecond
	return tw
}

func TestTwitchReadsChatAnonymously(t *testing.T) {
	f := &twitchFake{t: t}
	tw := testTwitch(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newRecSink()
	done := make(chan struct{})
	go func() { tw.run(ctx, out); close(done) }()
	// RECONNECT: a second session, which brings "after"
	waitFor(t, "the message after RECONNECT", func() bool { _, ok := out.s.get("twitch:after"); return ok })

	f.mu.Lock()
	login, pong, sessions := f.login, f.pong, f.sessions
	f.mu.Unlock()
	if len(login) != 4 || login[0] != "CAP REQ :twitch.tv/tags twitch.tv/commands" || login[1] != "PASS SCHMOOPIIE" ||
		!strings.HasPrefix(login[2], "NICK justinfan") || len(login[2]) != len("NICK justinfan12345") || login[3] != "JOIN #yourchurch" {
		t.Fatalf("login %q", login)
	}
	if pong != "PONG :tmi.twitch.tv\r\n" {
		t.Fatalf("pong %q", pong)
	}
	if sessions != 2 {
		t.Fatalf("%d sessions", sessions)
	}
	// m2 and m5 (spam, timed out) and m6 (deleted) are gone
	if got := ids(out.s.view(20).messages); got != "twitch:m1 twitch:m3 twitch:m4 twitch:after " {
		t.Fatalf("messages %q", got)
	}
	m3, _ := out.s.get("twitch:m3")
	m4, _ := out.s.get("twitch:m4")
	if m3.Super != "50 bits" || m4.Super != "Sub" || m4.Text != "Three months!" || m4.Author != "Ana" {
		t.Fatalf("cheer %+v sub %+v", m3, m4)
	}
	st := out.last()
	if st.State != stLive || st.Viewers != -1 || st.Note != twitchNote || !st.Info {
		t.Fatalf("status %+v", st)
	}
	cancel()
	<-done
}

func TestTwitchSuspendedChannelAndBackoff(t *testing.T) {
	f := &twitchFake{t: t, notice: "msg_channel_suspended"}
	tw := testTwitch(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	out := newRecSink()
	done := make(chan struct{})
	go func() { tw.run(ctx, out); close(done) }()
	waitFor(t, "three tries", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sessions >= 3 })
	st := out.last()
	if st.State != stError || !strings.Contains(st.Note, "suspended") || !strings.Contains(st.Note, "trying again") {
		t.Fatalf("status %+v", st)
	}
	cancel()
	<-done
	// nothing listening: an error, not a hang
	dead := &twitch{channel: "x", url: "ws://127.0.0.1:1", minBackoff: time.Hour, maxBackoff: time.Hour}
	ctx, cancel = context.WithCancel(context.Background())
	out = newRecSink()
	done = make(chan struct{})
	go func() { dead.run(ctx, out); close(done) }()
	waitFor(t, "an error status", func() bool { return out.last().State == stError })
	cancel()
	<-done
}
