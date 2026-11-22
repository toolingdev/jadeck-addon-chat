package main

// Twitch replies: sign in with Twitch's Device Code Grant flow (a public
// app: client id only, no secret), then send through the Helix API.
//
//   POST https://id.twitch.tv/oauth2/device  client_id, scopes=user:write:chat
//     → device_code, user_code, verification_uri, expires_in, interval
//   POST https://id.twitch.tv/oauth2/token   client_id, scopes, device_code,
//     grant_type=urn:ietf:params:oauth:grant-type:device_code
//     → 400 {"message":"authorization_pending"} until approved, then tokens
//   GET  https://api.twitch.tv/helix/users            (who signed in)
//   GET  https://api.twitch.tv/helix/users?login=<channel>  (broadcaster id)
//   POST https://api.twitch.tv/helix/chat/messages
//     {broadcaster_id, sender_id, message, reply_parent_message_id}
//
// Access tokens last about 4 hours. A public app's refresh token works
// once: every refresh saves the new one at once. It ends after 30 days,
// then the person signs in again.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

const (
	twitchIDBase    = "https://id.twitch.tv/oauth2"
	twitchHelixBase = "https://api.twitch.tv/helix"
	twitchScopes    = "user:write:chat"
)

// twitchReply is a Twitch chat connection's reply side.
type twitchReply struct {
	clientID  string
	idBase    string
	helixBase string
	hc        *http.Client
	store     *tokenStore

	mu  sync.Mutex
	dev *device
}

func (t *twitch) tokenKey() string { return "twitch:" + t.channel + ":" + t.r.clientID }

var errTwitchSignedOut = errors.New("not signed in (Settings → Twitch chat → Sign in to Twitch to reply)")

// canReply (replier): signed in.
func (t *twitch) canReply() (bool, string, string) {
	if t.r.clientID == "" {
		return false, "", "not signed in (needs a Twitch app client ID)"
	}
	e, ok := t.r.store.get(t.tokenKey())
	if !ok {
		return false, "", "not signed in"
	}
	return true, e.Account, ""
}

// twitchErr is Twitch's error JSON ({"status":400,"message":"…"}).
type twitchErr struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func readTwitchErr(body []byte) string {
	var e twitchErr
	json.Unmarshal(body, &e)
	if e.Message != "" {
		return e.Message
	}
	return e.Error
}

// signIn (signer): the "Sign in to Twitch to reply" field.
func (t *twitch) signIn(ctx context.Context, p signParams) addon.ActionFieldResult {
	r := t.r
	signedIn := func(e tokenEntry) addon.ActionFieldResult {
		return addon.ActionFieldResult{OK: true, Text: "Can reply as " + e.Account, Button: "Sign out"}
	}
	notIn := addon.ActionFieldResult{Text: "Not signed in: reading only. Sign in once with the account replies should come from.", Button: "Sign in to Twitch to reply"}
	if r.clientID == "" {
		return addon.ActionFieldResult{Text: "To reply, fill in \"Twitch app client ID\" (see How to set up), save, then sign in here.", Button: "Sign in to Twitch to reply"}
	}
	key := t.tokenKey()
	r.mu.Lock()
	dev := r.dev
	r.mu.Unlock()
	switch {
	case p.Status:
		if e, ok := r.store.get(key); ok {
			return signedIn(e)
		}
		if dev != nil && time.Now().Before(dev.expires) {
			return dev.waitResult("Twitch")
		}
		return notIn
	case p.Poll:
		if dev == nil {
			if e, ok := r.store.get(key); ok {
				return signedIn(e)
			}
			return notIn
		}
		return t.pollDevice(ctx, dev)
	}
	// A press: sign out when signed in, else start.
	if _, ok := r.store.get(key); ok {
		r.mu.Lock()
		r.dev = nil
		r.mu.Unlock()
		if err := r.store.remove(key); err != nil {
			return addon.ActionFieldResult{Error: "Couldn't sign out: " + err.Error(), Button: "Sign out"}
		}
		n := notIn
		n.Text = "Signed out: reading only."
		return n
	}
	status, body, err := postForm(ctx, r.hc, r.idBase+"/device", url.Values{"client_id": {r.clientID}, "scopes": {twitchScopes}})
	if err != nil {
		return addon.ActionFieldResult{Error: "Twitch didn't answer: " + err.Error(), Button: "Sign in to Twitch to reply"}
	}
	if status/100 != 2 {
		msg := readTwitchErr(body)
		if strings.Contains(strings.ToLower(msg), "client") {
			msg = "Twitch didn't accept the app client ID (check it, and that the app's Client Type is Public)"
		} else {
			msg = "Twitch said: " + msg
		}
		return addon.ActionFieldResult{Error: msg, Button: "Sign in to Twitch to reply"}
	}
	var d struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if json.Unmarshal(body, &d) != nil || d.DeviceCode == "" || d.UserCode == "" {
		return addon.ActionFieldResult{Error: "Twitch answered oddly; try again", Button: "Sign in to Twitch to reply"}
	}
	if d.VerificationURI == "" {
		d.VerificationURI = "https://www.twitch.tv/activate"
	}
	dev = &device{deviceCode: d.DeviceCode, userCode: d.UserCode, verifyURL: d.VerificationURI,
		expires: time.Now().Add(time.Duration(max(d.ExpiresIn, 60)) * time.Second), interval: time.Duration(max(d.Interval, 1)) * time.Second}
	dev.next = time.Now().Add(dev.interval)
	r.mu.Lock()
	r.dev = dev
	r.mu.Unlock()
	return dev.waitResult("Twitch")
}

// twitchToken is a token answer.
type twitchToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (t *twitch) pollDevice(ctx context.Context, dev *device) addon.ActionFieldResult {
	r := t.r
	retry := addon.ActionFieldResult{Button: "Sign in to Twitch to reply"}
	if time.Now().After(dev.expires) {
		t.clearDevice(dev)
		retry.Error = "The code ran out. Press Sign in again for a new one."
		return retry
	}
	r.mu.Lock()
	early := time.Now().Before(dev.next)
	r.mu.Unlock()
	if early {
		return dev.waitResult("Twitch")
	}
	status, body, err := postForm(ctx, r.hc, r.idBase+"/token", url.Values{"client_id": {r.clientID}, "scopes": {twitchScopes},
		"device_code": {dev.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}})
	r.mu.Lock()
	dev.next = time.Now().Add(dev.interval)
	r.mu.Unlock()
	if err != nil {
		return dev.waitResult("Twitch") // keep waiting through a hiccup
	}
	if status/100 != 2 {
		switch msg := readTwitchErr(body); msg {
		case "authorization_pending":
			return dev.waitResult("Twitch")
		case "slow_down":
			r.mu.Lock()
			dev.interval += 5 * time.Second
			r.mu.Unlock()
			return dev.waitResult("Twitch")
		case "access_denied":
			t.clearDevice(dev)
			retry.Error = "Sign-in was turned down on Twitch."
			return retry
		default:
			t.clearDevice(dev)
			retry.Error = "The code ran out or was used. Press Sign in again for a new one."
			return retry
		}
	}
	var tok twitchToken
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		t.clearDevice(dev)
		retry.Error = "Twitch answered oddly; press Sign in again."
		return retry
	}
	e := tokenEntry{Access: tok.AccessToken, Refresh: tok.RefreshToken, Expiry: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
	me, err := t.user(ctx, e.Access, "")
	if err != nil {
		t.clearDevice(dev)
		retry.Error = "Signed in, but Twitch didn't say who: " + err.Error()
		return retry
	}
	e.Account, e.UserID = me.DisplayName, me.ID
	if e.Account == "" {
		e.Account = me.Login
	}
	if ch, err := t.user(ctx, e.Access, t.channel); err == nil {
		e.ChannelID = ch.ID
	} else {
		t.clearDevice(dev)
		retry.Error = "Signed in, but Twitch has no channel " + t.channel
		return retry
	}
	t.clearDevice(dev)
	if err := r.store.put(t.tokenKey(), e); err != nil {
		retry.Error = "Signed in, but " + err.Error()
		return retry
	}
	return addon.ActionFieldResult{OK: true, Text: "Can reply as " + e.Account, Button: "Sign out"}
}

func (t *twitch) clearDevice(dev *device) {
	t.r.mu.Lock()
	if t.r.dev == dev {
		t.r.dev = nil
	}
	t.r.mu.Unlock()
}

type twitchUser struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
}

// user looks up the signed-in user (login "") or a channel.
func (t *twitch) user(ctx context.Context, access, login string) (twitchUser, error) {
	u := t.r.helixBase + "/users"
	if login != "" {
		u += "?login=" + url.QueryEscape(login)
	}
	var r struct {
		Data []twitchUser `json:"data"`
	}
	if _, err := bearerGet(ctx, t.r.hc, u, access, http.Header{"Client-Id": {t.r.clientID}}, &r); err != nil {
		return twitchUser{}, errors.New(redact(err.Error(), access))
	}
	if len(r.Data) == 0 {
		return twitchUser{}, errors.New("no such user")
	}
	return r.Data[0], nil
}

// access returns a usable access token, refreshing (and saving the new
// refresh token) when it ran out or force is set.
func (t *twitch) access(ctx context.Context, force bool) (tokenEntry, error) {
	key := t.tokenKey()
	l := keyLock(key)
	l.Lock()
	defer l.Unlock()
	e, ok := t.r.store.get(key)
	if !ok {
		return tokenEntry{}, errTwitchSignedOut
	}
	if !force && e.usable(time.Now()) {
		return e, nil
	}
	if e.Refresh == "" {
		t.r.store.remove(key)
		return tokenEntry{}, errors.New("the Twitch sign-in ran out; sign in again (Settings → Twitch chat)")
	}
	status, body, err := postForm(ctx, t.r.hc, t.r.idBase+"/token", url.Values{"grant_type": {"refresh_token"},
		"refresh_token": {e.Refresh}, "client_id": {t.r.clientID}})
	if err != nil {
		return tokenEntry{}, fmt.Errorf("couldn't renew the Twitch sign-in: %v", err)
	}
	var tok twitchToken
	if status/100 != 2 || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		if status == http.StatusBadRequest || status == http.StatusUnauthorized {
			t.r.store.remove(key)
			return tokenEntry{}, errors.New("the Twitch sign-in ran out (after 30 days, or it was disconnected); sign in again (Settings → Twitch chat)")
		}
		return tokenEntry{}, fmt.Errorf("couldn't renew the Twitch sign-in (%d)", status)
	}
	e2, ok, err := t.r.store.update(key, func(x *tokenEntry) {
		x.Access, x.Expiry = tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
		if tok.RefreshToken != "" {
			x.Refresh = tok.RefreshToken
		}
	})
	if err != nil {
		return tokenEntry{}, err
	}
	if !ok {
		return tokenEntry{}, errTwitchSignedOut
	}
	return e2, nil
}

// send (replier): Helix Send Chat Message; a reply names the comment.
func (t *twitch) send(ctx context.Context, text string, replyTo *Message) (string, error) {
	for attempt := 0; ; attempt++ {
		e, err := t.access(ctx, attempt > 0)
		if err != nil {
			return "", err
		}
		body := map[string]any{"broadcaster_id": e.ChannelID, "sender_id": e.UserID, "message": text}
		if replyTo != nil {
			if id, ok := strings.CutPrefix(replyTo.ID, Twitch+":"); ok {
				body["reply_parent_message_id"] = id
			}
		}
		status, b, err := postJSON(ctx, t.r.hc, t.r.helixBase+"/chat/messages",
			http.Header{"Authorization": {"Bearer " + e.Access}, "Client-Id": {t.r.clientID}}, body)
		if err != nil {
			return "", err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			continue // the token ran out early: renew once
		}
		if status/100 != 2 {
			msg := redact(readTwitchErr(b), e.Access, e.Refresh)
			switch status {
			case http.StatusUnauthorized:
				return "", errors.New("Twitch refused the sign-in (" + msg + "); sign in again")
			case http.StatusForbidden:
				return "", errors.New("this account isn't allowed to chat in " + t.channel + " (banned or timed out?)")
			case http.StatusTooManyRequests:
				return "", errors.New("sending too fast; wait a moment and retry")
			case 422:
				return "", errors.New("the message is too long for Twitch")
			}
			return "", errors.New("Twitch said: " + msg)
		}
		var r struct {
			Data []struct {
				MessageID  string `json:"message_id"`
				IsSent     bool   `json:"is_sent"`
				DropReason *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"drop_reason"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &r) != nil || len(r.Data) == 0 {
			return "", errors.New("Twitch answered oddly")
		}
		d := r.Data[0]
		if !d.IsSent {
			why := "Twitch held it back"
			if d.DropReason != nil && d.DropReason.Message != "" {
				why = "Twitch held it back: " + d.DropReason.Message
			}
			return "", errors.New(why)
		}
		if d.MessageID == "" {
			return "", nil
		}
		return Twitch + ":" + d.MessageID, nil
	}
}
