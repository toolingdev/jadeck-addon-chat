package main

// Owncast chat: read as an anonymous chat user named as "Reply as" says
// ("jadeck" by default); replies go out under that name.
//
//   POST <server>/api/chat/register {"displayName":"jadeck"} → {id, accessToken}
//   websocket <server>/ws?accessToken=…  → {"type":"CHAT", id, body (HTML),
//                                           user{displayName}, timestamp}
//   GET <server>/api/status → {online, viewerCount, streamTitle}, every 30 s
//   send: {"type":"CHAT","body":"…"} over the same websocket. Owncast
//   doesn't answer a send: it shows the message to everyone (our own
//   user id comes back), or tells only us why not with a CHAT_ACTION
//   ("Authentication is required to participate in chat.").
//
// NOTE: /api/chat/register is what Owncast's own web chat uses; it is
// not part of Owncast's documented, stable API and may change between
// Owncast versions. If it fails, the status says the server didn't
// accept an anonymous chat login.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type owncast struct {
	name        string // the chat name ("Reply as")
	base        string // scheme://host[:port][/path], no trailing slash
	hc          *http.Client
	statusEvery time.Duration
	minBackoff  time.Duration
	maxBackoff  time.Duration
	r           *owncastReply
}

// owncastBase reads the server address as typed in a browser.
func owncastBase(addr string) (string, error) {
	a := strings.TrimSpace(addr)
	if a == "" {
		return "", errors.New("Owncast: enter your server's address (like https://live.example.org)")
	}
	if !strings.Contains(a, "://") {
		// No scheme: a LAN address is plain http, anything else https.
		host := a
		if i := strings.IndexAny(host, "/"); i >= 0 {
			host = host[:i]
		}
		h, _, err := net.SplitHostPort(host)
		if err != nil {
			h = host
		}
		if ip := net.ParseIP(h); (ip != nil && (ip.IsPrivate() || ip.IsLoopback())) || h == "localhost" {
			a = "http://" + a
		} else {
			a = "https://" + a
		}
	}
	u, err := url.Parse(a)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("Owncast: %q isn't a server address (like https://live.example.org)", addr)
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func newOwncast(addr string) (*owncast, error) {
	base, err := owncastBase(addr)
	if err != nil {
		return nil, err
	}
	return &owncast{name: "jadeck", r: &owncastReply{}, base: base, hc: defaultClient, statusEvery: 30 * time.Second, minBackoff: time.Second, maxBackoff: 60 * time.Second}, nil
}

func (o *owncast) platform() string { return Owncast }

// label is the server's host, for the widget's chip.
func (o *owncast) label() string {
	if u, err := url.Parse(o.base); err == nil {
		return u.Host
	}
	return o.base
}

var errOwncastLogin = errors.New("this Owncast server didn't accept an anonymous chat login")

// register gets an anonymous chat access token (and our user id).
func (o *owncast) register(ctx context.Context) (token, userID string, err error) {
	nb, _ := json.Marshal(map[string]string{"displayName": o.name})
	body := bytes.NewBuffer(nb)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/api/chat/register", body)
	if err != nil {
		return "", "", errors.New("bad server address")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := o.hc.Do(req)
	if err != nil {
		return "", "", scrub(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var r struct {
		ID          string `json:"id"`
		AccessToken string `json:"accessToken"`
	}
	if res.StatusCode/100 != 2 || json.Unmarshal(b, &r) != nil || r.AccessToken == "" {
		return "", "", fmt.Errorf("%w (%s)", errOwncastLogin, res.Status)
	}
	return r.AccessToken, r.ID, nil
}

type owncastStatus struct {
	Online      bool   `json:"online"`
	ViewerCount int    `json:"viewerCount"`
	StreamTitle string `json:"streamTitle"`
}

func (o *owncast) status(ctx context.Context) (owncastStatus, error) {
	var s owncastStatus
	err := getJSON(ctx, o.hc, o.base+"/api/status", nil, &s, func(status int, _ []byte) error { return &apiError{Status: status} })
	return s, err
}

// owncastEvent is one websocket message.
type owncastEvent struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	Body      string   `json:"body"`
	Timestamp string   `json:"timestamp"`
	Visible   *bool    `json:"visible"`
	IDs       []string `json:"ids"` // VISIBILITY-UPDATE: messages hidden by a moderator
	User      *struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
}

var (
	reImgAlt = regexp.MustCompile(`(?i)<img[^>]*\balt="([^"]*)"[^>]*>`)
	reTags   = regexp.MustCompile(`<[^>]*>`)
	reSpaces = regexp.MustCompile(`\s+`)
)

// plainText turns a chat body (HTML) into text; emoji pictures become
// their alt text.
func plainText(s string) string {
	s = reImgAlt.ReplaceAllString(s, " $1 ")
	s = strings.NewReplacer("<br>", " ", "<br/>", " ", "<br />", " ", "</p><p>", " ").Replace(s)
	s = reTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// parseOwncast reads one websocket message (Owncast may send several,
// one per line): the chat messages, and ids a moderator hid.
func parseOwncast(raw string) (msgs []Message, hidden []string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e owncastEvent
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Type {
		case "CHAT", "SYSTEM":
			if e.ID == "" || (e.Visible != nil && !*e.Visible) {
				continue
			}
			m := Message{ID: Owncast + ":" + e.ID, Platform: Owncast, Text: plainText(e.Body), Author: "Owncast"}
			if e.User != nil && strings.TrimSpace(e.User.DisplayName) != "" {
				m.Author = strings.TrimSpace(e.User.DisplayName)
			}
			if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
				m.At = t
			}
			msgs = append(msgs, m)
		case "VISIBILITY-UPDATE":
			if e.Visible != nil && !*e.Visible {
				for _, id := range e.IDs {
					hidden = append(hidden, Owncast+":"+id)
				}
			}
			// CHAT_ACTION (someone joined, renamed…) and the rest: skipped.
		}
	}
	return msgs, hidden
}

// owncastConfig is the part of /api/config the chat needs: a server
// behind a CDN names a separate host for the websocket
// (socketHostOverride), and chat can be turned off.
type owncastConfig struct {
	SocketHostOverride string `json:"socketHostOverride"`
	ChatDisabled       bool   `json:"chatDisabled"`
}

var errOwncastChatOff = errors.New("chat is turned off on this Owncast server")

// socketBase is where the websocket lives: the server's
// socketHostOverride when it sets one (an http(s) address), else the
// server itself. A config that can't be read falls back to the server.
func (o *owncast) socketBase(ctx context.Context) (string, error) {
	var c owncastConfig
	if err := getJSON(ctx, o.hc, o.base+"/api/config", nil, &c, func(status int, _ []byte) error { return &apiError{Status: status} }); err != nil {
		return o.base, nil
	}
	if c.ChatDisabled {
		return "", errOwncastChatOff
	}
	if h := strings.TrimRight(strings.TrimSpace(c.SocketHostOverride), "/"); h != "" {
		if u, err := url.Parse(h); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return u.Scheme + "://" + u.Host, nil
		}
	}
	return o.base, nil
}

// wsURL is the chat websocket's address.
func (o *owncast) wsURL(base, token string) string {
	u := base
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + "/ws?accessToken=" + url.QueryEscape(token)
}

func (o *owncast) run(ctx context.Context, out sink) {
	backoff := o.minBackoff
	for ctx.Err() == nil {
		ok, err := o.session(ctx, out)
		if ctx.Err() != nil {
			return
		}
		if ok {
			backoff = o.minBackoff
		}
		note := "Owncast: " + err.Error()
		out.status(sourceStatus{Platform: Owncast, State: stError, Viewers: -1,
			Note: fmt.Sprintf("%s; trying again in %s", note, backoff.Round(time.Second))})
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, o.maxBackoff)
	}
}

// session is one chat connection; ok when the chat was connected.
func (o *owncast) session(ctx context.Context, out sink) (ok bool, err error) {
	token, me, err := o.register(ctx)
	if err != nil {
		return false, err
	}
	base, err := o.socketBase(ctx)
	if err != nil {
		return false, err
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ws, err := wsDial(sctx, o.wsURL(base, token), nil)
	if err != nil {
		return false, errors.New(redact(err.Error(), token))
	}
	defer ws.Close()
	o.r.connect(ws, me)
	defer o.r.disconnect(ws)

	type read struct {
		raw string
		err error
	}
	reads := make(chan read)
	go func() {
		for {
			raw, err := ws.read()
			select {
			case reads <- read{raw, err}:
			case <-sctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	report := func() {
		st := sourceStatus{Platform: Owncast, State: stWaiting, Viewers: -1, Note: "chat connected, waiting for the stream to go live"}
		s, err := o.status(sctx)
		switch {
		case sctx.Err() != nil:
			return
		case err != nil:
			st.Note = "chat connected (no stream status: " + err.Error() + ")"
		case s.Online:
			st = sourceStatus{Platform: Owncast, State: stLive, Viewers: s.ViewerCount, Title: s.StreamTitle}
		}
		out.status(st)
	}
	report()
	tick := time.NewTicker(o.statusEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-tick.C:
			report()
		case r := <-reads:
			if r.err != nil {
				return true, r.err
			}
			msgs, hidden := parseOwncast(r.raw)
			msgs = o.r.own(r.raw, msgs)
			out.add(msgs...)
			for _, id := range hidden {
				out.remove(id)
			}
		}
	}
}
