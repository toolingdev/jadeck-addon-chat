package main

// Facebook live comments over the Graph API with a Page access token:
//   - the Page's live video: /{page-id}/live_videos?broadcast_status=["LIVE"]
//     every minute while nothing is live;
//   - its comments: /{live-video-id}/comments (reverse chronological,
//     live_filter=no_filter, since the newest seen) every few seconds;
//   - viewers and the end: /{live-video-id}?fields=status,live_views a minute.
// The token rides in the query string (Graph API's usual way); errors
// never print a URL and are scrubbed of it anyway.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// graphAPI is the Graph API version used (each lasts about two years).
const graphAPI = "https://graph.facebook.com/v23.0"

var rePageID = regexp.MustCompile(`^[A-Za-z0-9.]{1,80}$`)

type facebook struct {
	r           *facebookReply
	page, token string
	base        string
	hc          *http.Client
	pollEvery   time.Duration // comments
	checkEvery  time.Duration // looking for a live video
	statsEvery  time.Duration // viewers / end
	limitWait   time.Duration // after a rate limit
}

func newFacebook(page, token string) (*facebook, error) {
	page, token = strings.TrimSpace(page), strings.TrimSpace(token)
	if page == "" {
		return nil, errors.New("Facebook: enter the Page ID")
	}
	if !rePageID.MatchString(page) {
		return nil, fmt.Errorf("Facebook: %q isn't a Page ID (digits, from the Page's About → Page transparency)", page)
	}
	if token == "" {
		return nil, errors.New("Facebook: enter the Page access token")
	}
	return &facebook{r: &facebookReply{}, page: page, token: token, base: graphAPI, hc: defaultClient,
		pollEvery: 5 * time.Second, checkEvery: 60 * time.Second, statsEvery: 60 * time.Second, limitWait: 5 * time.Minute}, nil
}

func (f *facebook) platform() string { return Facebook }

type fbErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (f *facebook) get(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("access_token", f.token)
	return getJSON(ctx, f.hc, f.base+path+"?"+q.Encode(), nil, out, func(status int, body []byte) error {
		var b fbErrorBody
		json.Unmarshal(body, &b)
		return &apiError{Status: status, Code: b.Error.Code, Message: redact(b.Error.Message, f.token)}
	})
}

func fbCode(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// fbLimited: Graph API rate limits (app, user, page, custom).
func fbLimited(err error) bool {
	switch fbCode(err) {
	case 4, 17, 32, 613, 80001:
		return true
	}
	return false
}

func (f *facebook) problem(err error) string {
	switch c := fbCode(err); {
	case c == 190:
		return "Facebook didn't accept the Page access token (expired or revoked)"
	case c == 10 || c == 200 || (c >= 200 && c < 300):
		return "The Facebook token lacks a permission (pages_read_engagement, pages_read_user_content)"
	case c == 100:
		return "Facebook: check the Page ID — " + err.Error()
	case fbLimited(err):
		return "Facebook is rate-limiting this token; trying again in a few minutes"
	}
	return "Facebook: " + err.Error()
}

type fbLive struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	LiveViews *int   `json:"live_views"`
	Video     *struct {
		ID string `json:"id"`
	} `json:"video"` // the underlying video (where replies to everyone are posted)
}

func (l fbLive) viewers() int {
	if l.LiveViews == nil {
		return -1
	}
	return *l.LiveViews
}

// findLive finds the Page's live video, or errNotLive.
func (f *facebook) findLive(ctx context.Context) (fbLive, error) {
	var r struct {
		Data []fbLive `json:"data"`
	}
	q := url.Values{"broadcast_status": {`["LIVE"]`}, "fields": {"id,title,status,live_views,video"}, "limit": {"5"}}
	if err := f.get(ctx, "/"+f.page+"/live_videos", q, &r); err != nil {
		return fbLive{}, err
	}
	for _, l := range r.Data {
		if l.ID != "" && (l.Status == "" || l.Status == "LIVE") {
			return l, nil
		}
	}
	return fbLive{}, errNotLive
}

type fbComments struct {
	Data []struct {
		ID          string `json:"id"`
		Message     string `json:"message"`
		CreatedTime string `json:"created_time"`
		From        *struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Picture *struct {
				Data struct {
					URL string `json:"url"`
				} `json:"data"`
			} `json:"picture"`
		} `json:"from"`
	} `json:"data"`
}

// fbTime is Graph API's time format (2026-11-21T19:42:10+0000).
const fbTime = "2006-01-02T15:04:05-0700"

// parseFacebookComments turns a comments answer into messages.
func parseFacebookComments(r fbComments) []Message {
	var out []Message
	for _, c := range r.Data {
		m := Message{ID: Facebook + ":" + c.ID, Platform: Facebook, Text: c.Message, Author: "Facebook user"}
		if t, err := time.Parse(fbTime, c.CreatedTime); err == nil {
			m.At = t
		} else if t, err := time.Parse(time.RFC3339, c.CreatedTime); err == nil {
			m.At = t
		}
		if c.From != nil {
			if n := strings.TrimSpace(c.From.Name); n != "" {
				m.Author = n
			}
			if c.From.Picture != nil {
				m.Avatar = c.From.Picture.Data.URL
			}
		}
		out = append(out, m)
	}
	return out
}

func (f *facebook) comments(ctx context.Context, liveID string, since time.Time) ([]Message, error) {
	q := url.Values{"order": {"reverse_chronological"}, "live_filter": {"no_filter"}, "filter": {"stream"},
		"fields": {"id,message,created_time,from"}, "limit": {"100"}}
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	var r fbComments
	if err := f.get(ctx, "/"+liveID+"/comments", q, &r); err != nil {
		return nil, err
	}
	return parseFacebookComments(r), nil
}

func (f *facebook) run(ctx context.Context, out sink) {
	f.checkPerms(ctx) // can replies post? (and the Page's name)
	for ctx.Err() == nil {
		lv, err := f.findLive(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			st, wait := sourceStatus{Platform: Facebook, State: stWaiting, Note: "No Facebook live video right now", Viewers: -1}, f.checkEvery
			if !errors.Is(err, errNotLive) {
				st.State, st.Note = stError, f.problem(err)
				if fbLimited(err) {
					wait = f.limitWait
				}
			}
			out.status(st)
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		st := sourceStatus{Platform: Facebook, State: stLive, Viewers: lv.viewers(), Title: lv.Title}
		out.status(st)
		f.follow(ctx, lv, out, st)
	}
}

// follow polls one live video's comments until it ends.
func (f *facebook) follow(ctx context.Context, lv fbLive, out sink, st sourceStatus) {
	video := ""
	if lv.Video != nil {
		video = lv.Video.ID
	}
	f.setLive(lv.ID, video)
	defer f.setLive("", "")
	var newest time.Time
	fails, stats := 0, time.Now()
	for ctx.Err() == nil {
		// A little overlap: comments in the same second as the newest
		// come again and are dropped as duplicates.
		since := newest
		if !since.IsZero() {
			since = since.Add(-2 * time.Second)
		}
		msgs, err := f.comments(ctx, lv.ID, since)
		if ctx.Err() != nil {
			return
		}
		wait := f.pollEvery
		if err != nil {
			fails++
			st.Note = f.problem(err)
			out.status(st)
			if fbCode(err) == 190 {
				st.State = stError
				out.status(st)
				sleep(ctx, f.limitWait)
				return
			}
			if fbLimited(err) {
				wait = f.limitWait
			} else {
				wait = min(f.pollEvery*time.Duration(1<<min(fails, 4)), 2*time.Minute)
			}
		} else {
			if fails > 0 || st.Note != "" {
				fails, st.Note = 0, ""
				out.status(st)
			}
			for _, m := range msgs {
				if m.At.After(newest) {
					newest = m.At
				}
			}
			out.add(msgs...)
		}
		if time.Since(stats) >= f.statsEvery {
			stats = time.Now()
			var cur fbLive
			if err := f.get(ctx, "/"+lv.ID, url.Values{"fields": {"id,status,live_views"}}, &cur); err == nil {
				if cur.Status != "" && cur.Status != "LIVE" {
					return
				}
				st.Viewers = cur.viewers()
				out.status(st)
			}
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}
