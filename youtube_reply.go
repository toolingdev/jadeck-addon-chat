package main

// YouTube replies: sign in with Google's OAuth 2.0 flow for "TVs and
// Limited Input devices" (a code typed on any phone or computer), scope
// https://www.googleapis.com/auth/youtube (the device flow allows it; it
// doesn't allow youtube.force-ssl), then liveChatMessages.insert on the
// live chat being read.
//
//   POST https://oauth2.googleapis.com/device/code  client_id, scope
//     → device_code, user_code, verification_url, expires_in, interval
//   POST https://oauth2.googleapis.com/token  client_id, client_secret,
//     device_code, grant_type=urn:ietf:params:oauth:grant-type:device_code
//     → {"error":"authorization_pending"} (428) until approved; slow_down;
//       access_denied; expired_token
//   GET  /youtube/v3/channels?part=snippet&mine=true   (who signed in, 1 unit)
//   POST /youtube/v3/liveChat/messages?part=snippet    (20 units)
//     {snippet:{liveChatId, type:textMessageEvent, textMessageDetails:{messageText}}}
//
// YouTube chat has no threads: a reply is "@author text". The token goes
// in an Authorization header; the API key stays for reading.

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
	googleOAuthBase = "https://oauth2.googleapis.com"
	youtubeScope    = "https://www.googleapis.com/auth/youtube"
)

// youtubeReply is a YouTube chat connection's reply side.
type youtubeReply struct {
	clientID, secret string
	oauthBase        string
	store            *tokenStore

	mu     sync.Mutex
	dev    *device
	chatID string // the live chat being read ("" nothing live)
}

func (y *youtube) tokenKey() string { return "youtube:" + y.id + ":" + y.r.clientID }

func (y *youtube) setChat(id string) {
	y.r.mu.Lock()
	y.r.chatID = id
	y.r.mu.Unlock()
}

// canReply (replier): signed in, and a live chat to post in.
func (y *youtube) canReply() (bool, string, string) {
	if y.r.clientID == "" || y.r.secret == "" {
		return false, "", "not signed in (needs an OAuth client ID and secret)"
	}
	e, ok := y.r.store.get(y.tokenKey())
	if !ok {
		return false, "", "not signed in"
	}
	y.r.mu.Lock()
	live := y.r.chatID != ""
	y.r.mu.Unlock()
	if !live {
		return false, e.Account, "not live right now"
	}
	return true, e.Account, ""
}

// googleErr is Google's OAuth error JSON.
type googleErr struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

const ytSignInLabel = "Sign in with Google to reply"

// signIn (signer): the "Sign in to reply" field.
func (y *youtube) signIn(ctx context.Context, p signParams) addon.ActionFieldResult {
	r := y.r
	if r.clientID == "" || r.secret == "" {
		return addon.ActionFieldResult{Text: "To reply, fill in \"OAuth client ID\" and \"OAuth client secret\" (see How to set up), save, then sign in here.", Button: ytSignInLabel}
	}
	key := y.tokenKey()
	signedIn := func(e tokenEntry) addon.ActionFieldResult {
		return addon.ActionFieldResult{OK: true, Text: "Can reply as " + e.Account, Button: "Sign out"}
	}
	notIn := addon.ActionFieldResult{Text: "Not signed in: reading only. Sign in once with the Google account of the channel.", Button: ytSignInLabel}
	r.mu.Lock()
	dev := r.dev
	r.mu.Unlock()
	switch {
	case p.Status:
		if e, ok := r.store.get(key); ok {
			return signedIn(e)
		}
		if dev != nil && time.Now().Before(dev.expires) {
			return dev.waitResult("Google")
		}
		return notIn
	case p.Poll:
		if dev == nil {
			if e, ok := r.store.get(key); ok {
				return signedIn(e)
			}
			return notIn
		}
		return y.pollDevice(ctx, dev)
	}
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
	status, body, err := postForm(ctx, y.hc, r.oauthBase+"/device/code", url.Values{"client_id": {r.clientID}, "scope": {youtubeScope}})
	if err != nil {
		return addon.ActionFieldResult{Error: "Google didn't answer: " + err.Error(), Button: ytSignInLabel}
	}
	var d struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if status/100 != 2 || json.Unmarshal(body, &d) != nil || d.DeviceCode == "" {
		var ge googleErr
		json.Unmarshal(body, &ge)
		msg := "Google said: " + firstNonEmpty(ge.Description, ge.Error, http.StatusText(status))
		if ge.Error == "invalid_client" || ge.Error == "unauthorized_client" {
			msg = "Google didn't accept the OAuth client ID (it must be the type \"TVs and Limited Input devices\")"
		}
		if ge.Error == "invalid_scope" {
			msg = "Google won't give this client the YouTube permission (is the YouTube Data API v3 on for its project?)"
		}
		return addon.ActionFieldResult{Error: msg, Button: ytSignInLabel}
	}
	if d.VerificationURL == "" {
		d.VerificationURL = "https://www.google.com/device"
	}
	dev = &device{deviceCode: d.DeviceCode, userCode: d.UserCode, verifyURL: d.VerificationURL,
		expires: time.Now().Add(time.Duration(max(d.ExpiresIn, 60)) * time.Second), interval: time.Duration(max(d.Interval, 1)) * time.Second}
	dev.next = time.Now().Add(dev.interval)
	r.mu.Lock()
	r.dev = dev
	r.mu.Unlock()
	return dev.waitResult("Google")
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

type googleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (y *youtube) clearDevice(dev *device) {
	y.r.mu.Lock()
	if y.r.dev == dev {
		y.r.dev = nil
	}
	y.r.mu.Unlock()
}

func (y *youtube) pollDevice(ctx context.Context, dev *device) addon.ActionFieldResult {
	r := y.r
	retry := addon.ActionFieldResult{Button: ytSignInLabel}
	if time.Now().After(dev.expires) {
		y.clearDevice(dev)
		retry.Error = "The code ran out. Press Sign in again for a new one."
		return retry
	}
	r.mu.Lock()
	early := time.Now().Before(dev.next)
	r.mu.Unlock()
	if early {
		return dev.waitResult("Google")
	}
	status, body, err := postForm(ctx, y.hc, r.oauthBase+"/token", url.Values{"client_id": {r.clientID}, "client_secret": {r.secret},
		"device_code": {dev.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}})
	r.mu.Lock()
	dev.next = time.Now().Add(dev.interval)
	r.mu.Unlock()
	if err != nil {
		return dev.waitResult("Google")
	}
	if status/100 != 2 {
		var ge googleErr
		json.Unmarshal(body, &ge)
		switch ge.Error {
		case "authorization_pending":
			return dev.waitResult("Google")
		case "slow_down":
			r.mu.Lock()
			dev.interval += 5 * time.Second
			r.mu.Unlock()
			return dev.waitResult("Google")
		case "access_denied":
			y.clearDevice(dev)
			retry.Error = "Sign-in was turned down on Google."
		case "invalid_client":
			y.clearDevice(dev)
			retry.Error = "Google didn't accept the OAuth client ID and secret."
		default:
			y.clearDevice(dev)
			retry.Error = "The code ran out or was used. Press Sign in again for a new one."
		}
		return retry
	}
	var tok googleToken
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		y.clearDevice(dev)
		retry.Error = "Google answered oddly; press Sign in again."
		return retry
	}
	e := tokenEntry{Access: tok.AccessToken, Refresh: tok.RefreshToken, Expiry: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
	var ch struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if _, err := bearerGet(ctx, y.hc, y.base+"/channels?part=snippet&mine=true", e.Access, nil, &ch); err != nil {
		y.clearDevice(dev)
		retry.Error = "Signed in, but YouTube didn't say which channel: " + redact(err.Error(), e.Access)
		return retry
	}
	if len(ch.Items) == 0 {
		y.clearDevice(dev)
		retry.Error = "That Google account has no YouTube channel; sign in with the channel's account."
		return retry
	}
	e.Account = ch.Items[0].Snippet.Title
	y.clearDevice(dev)
	if err := r.store.put(y.tokenKey(), e); err != nil {
		retry.Error = "Signed in, but " + err.Error()
		return retry
	}
	return addon.ActionFieldResult{OK: true, Text: "Can reply as " + e.Account, Button: "Sign out"}
}

// access returns a usable access token, refreshing it when needed.
func (y *youtube) access(ctx context.Context, force bool) (tokenEntry, error) {
	key := y.tokenKey()
	l := keyLock(key)
	l.Lock()
	defer l.Unlock()
	e, ok := y.r.store.get(key)
	if !ok {
		return tokenEntry{}, errors.New("not signed in (Settings → YouTube chat → Sign in with Google to reply)")
	}
	if !force && e.usable(time.Now()) {
		return e, nil
	}
	if e.Refresh == "" {
		y.r.store.remove(key)
		return tokenEntry{}, errors.New("the Google sign-in ran out; sign in again (Settings → YouTube chat)")
	}
	status, body, err := postForm(ctx, y.hc, y.r.oauthBase+"/token", url.Values{"grant_type": {"refresh_token"},
		"refresh_token": {e.Refresh}, "client_id": {y.r.clientID}, "client_secret": {y.r.secret}})
	if err != nil {
		return tokenEntry{}, fmt.Errorf("couldn't renew the Google sign-in: %v", err)
	}
	var tok googleToken
	if status/100 != 2 || json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		var ge googleErr
		json.Unmarshal(body, &ge)
		if ge.Error == "invalid_grant" {
			y.r.store.remove(key)
			return tokenEntry{}, errors.New("the Google sign-in ended (while the OAuth app is in Testing, Google ends it after 7 days); sign in again (Settings → YouTube chat)")
		}
		return tokenEntry{}, fmt.Errorf("couldn't renew the Google sign-in (%d)", status)
	}
	e2, ok, err := y.r.store.update(key, func(x *tokenEntry) {
		x.Access, x.Expiry = tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
		if tok.RefreshToken != "" {
			x.Refresh = tok.RefreshToken
		}
	})
	if err != nil {
		return tokenEntry{}, err
	}
	if !ok {
		return tokenEntry{}, errors.New("signed out")
	}
	return e2, nil
}

// send (replier): liveChatMessages.insert in the chat being read.
func (y *youtube) send(ctx context.Context, text string, _ *Message) (string, error) {
	y.r.mu.Lock()
	chat := y.r.chatID
	y.r.mu.Unlock()
	if chat == "" {
		return "", errors.New("not live right now")
	}
	body := map[string]any{"snippet": map[string]any{"liveChatId": chat, "type": "textMessageEvent",
		"textMessageDetails": map[string]any{"messageText": text}}}
	for attempt := 0; ; attempt++ {
		e, err := y.access(ctx, attempt > 0)
		if err != nil {
			return "", err
		}
		status, b, err := postJSON(ctx, y.hc, y.base+"/liveChat/messages?part=snippet", http.Header{"Authorization": {"Bearer " + e.Access}}, body)
		if err != nil {
			return "", err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if status/100 != 2 {
			var eb ytErrorBody
			json.Unmarshal(b, &eb)
			reason := ""
			if len(eb.Error.Errors) > 0 {
				reason = eb.Error.Errors[0].Reason
			}
			switch reason {
			case "liveChatEnded", "liveChatNotFound":
				return "", errors.New("the live chat has ended")
			case "liveChatDisabled":
				return "", errors.New("chat is turned off for this stream")
			case "forbidden", "insufficientPermissions":
				return "", errors.New("this account can't post in that chat (sign in with the channel's account)")
			case "rateLimitExceeded":
				return "", errors.New("sending too fast; wait a moment and retry")
			case "quotaExceeded", "dailyLimitExceeded":
				return "", errors.New("the API quota for today is used up")
			case "messageTextInvalid":
				return "", errors.New("YouTube didn't accept that text")
			}
			msg := redact(eb.Error.Message, e.Access, y.key)
			if msg == "" {
				msg = http.StatusText(status)
			}
			return "", errors.New("YouTube said: " + strings.TrimSpace(msg))
		}
		var r struct {
			ID string `json:"id"`
		}
		json.Unmarshal(b, &r)
		if r.ID == "" {
			return "", nil
		}
		return YouTube + ":" + r.ID, nil
	}
}
