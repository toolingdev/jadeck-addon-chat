package main

// Facebook replies, with the Page token the connection already has (no
// sign-in): the comment is posted as the Page.
//
//   reply to one comment:  POST /{comment-id}/comments  message=…
//   message everyone:      POST /{video-id}/comments    message=…
//     (the live video's underlying video; a LiveVideo's own comments
//     edge can only be read)
//   what the token can do: GET /debug_token?input_token=<token>, its
//     scopes; GET /me?fields=id,name for the Page's name
//
// Posting needs the pages_manage_engagement permission on the token.
// The token travels in the POST body (and, for the checks, the query
// string as reading does); errors are scrubbed of it.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// Permission check results.
const (
	fbPermUnknown = iota // not checked, or Facebook wouldn't say
	fbPermOK
	fbPermMissing
)

// facebookReply is a Facebook Page chat connection's reply side.
type facebookReply struct {
	mu       sync.Mutex
	liveID   string // the live video being read ("" none)
	videoID  string // its video (where comments are posted)
	pageName string
	perm     int
	checked  time.Time
}

const fbPermMissingText = "the Page token can't post yet; add the pages_manage_engagement permission (Settings → Facebook chat)"

func (f *facebook) setLive(liveID, videoID string) {
	f.r.mu.Lock()
	f.r.liveID, f.r.videoID = liveID, videoID
	f.r.mu.Unlock()
}

// canReply (replier): the token can post (or we couldn't tell) and a
// live video is up.
func (f *facebook) canReply() (bool, string, string) {
	f.r.mu.Lock()
	defer f.r.mu.Unlock()
	name := f.r.pageName
	if f.r.perm == fbPermMissing {
		return false, name, "the Page token lacks pages_manage_engagement"
	}
	if f.r.liveID == "" {
		return false, name, "not live right now"
	}
	return true, name, ""
}

// checkPerms learns the Page's name and whether the token has
// pages_manage_engagement. debug_token is documented for app and app
// developer tokens; when Facebook won't answer it for a Page token, the
// permission stays unknown and sends simply try.
func (f *facebook) checkPerms(ctx context.Context) {
	var me struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	nameErr := f.get(ctx, "/me", url.Values{"fields": {"id,name"}}, &me)
	var dbg struct {
		Data struct {
			IsValid bool     `json:"is_valid"`
			Scopes  []string `json:"scopes"`
		} `json:"data"`
	}
	perm := fbPermUnknown
	if err := f.get(ctx, "/debug_token", url.Values{"input_token": {f.token}}, &dbg); err == nil && dbg.Data.IsValid && len(dbg.Data.Scopes) > 0 {
		perm = fbPermMissing
		if slices.Contains(dbg.Data.Scopes, "pages_manage_engagement") {
			perm = fbPermOK
		}
	}
	f.r.mu.Lock()
	defer f.r.mu.Unlock()
	if nameErr == nil && me.Name != "" {
		f.r.pageName = me.Name
	}
	f.r.perm, f.r.checked = perm, time.Now()
}

// signIn (signer): the status field ("Can reply as <Page>" or what's missing).
func (f *facebook) signIn(ctx context.Context, p signParams) addon.ActionFieldResult {
	f.checkPerms(ctx)
	f.r.mu.Lock()
	name, perm := f.r.pageName, f.r.perm
	f.r.mu.Unlock()
	if name == "" {
		name = "the Page"
	}
	res := addon.ActionFieldResult{Button: "Check again"}
	switch perm {
	case fbPermOK:
		res.OK, res.Text = true, "Can reply as "+name
	case fbPermMissing:
		res.Error = "Reading works. To reply, the Page token needs pages_manage_engagement: get a new Page token with it (How to set up, step 2) and paste it above."
	default:
		res.Text = "Replies post as " + name + ". Facebook didn't say whether the token has pages_manage_engagement; if a reply fails, add it (How to set up, step 2)."
	}
	return res
}

// send (replier): a reply under one comment, or a comment on the live video.
func (f *facebook) send(ctx context.Context, text string, replyTo *Message) (string, error) {
	f.r.mu.Lock()
	target := f.r.videoID
	if target == "" {
		target = f.r.liveID
	}
	f.r.mu.Unlock()
	if replyTo != nil {
		id, ok := strings.CutPrefix(replyTo.ID, Facebook+":")
		if !ok || id == "" {
			return "", errors.New("that isn't a Facebook comment")
		}
		target = id
	}
	if target == "" {
		return "", errors.New("not live right now")
	}
	status, body, err := postForm(ctx, f.hc, f.base+"/"+target+"/comments", url.Values{"message": {text}, "access_token": {f.token}})
	if err != nil {
		return "", errors.New(redact(err.Error(), f.token))
	}
	if status/100 != 2 {
		var b fbErrorBody
		json.Unmarshal(body, &b)
		e := &apiError{Status: status, Code: b.Error.Code, Message: redact(b.Error.Message, f.token)}
		switch c := e.Code; {
		case c == 10 || c == 200 || (c > 200 && c < 300):
			f.r.mu.Lock()
			f.r.perm = fbPermMissing
			f.r.mu.Unlock()
			return "", errors.New(fbPermMissingText)
		case c == 190:
			return "", errors.New("Facebook didn't accept the Page access token (expired or revoked)")
		case fbLimited(e):
			return "", errors.New("Facebook is rate-limiting this token; retry in a few minutes")
		}
		if e.Message == "" {
			e.Message = http.StatusText(status)
		}
		return "", errors.New("Facebook said: " + e.Message)
	}
	var r struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &r)
	if r.ID == "" {
		return "", nil
	}
	return Facebook + ":" + r.ID, nil
}
