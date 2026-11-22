package main

// Rumble chat through the channel's Live Stream API link (Account →
// Live Stream API on rumble.com). The link carries a key, so it is
// treated like a password: never logged, never in an error.
//
// GET <link> answers JSON with livestreams[]: is_live, title,
// watching_now and chat.recent_messages[] / chat.recent_rants[] (the
// last 50 or so). Polled every 5 s while a stream is live, every 30 s
// while none is. Messages have no id, so one is made from
// created_on + username + text.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type rumble struct {
	link      string
	hc        *http.Client
	liveEvery time.Duration
	idleEvery time.Duration

	seen  map[string]bool
	order []string // seen ids, oldest first (bounded)
}

// rumbleSeenMax bounds the seen-set.
const rumbleSeenMax = 2000

func newRumble(link string) (*rumble, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, errors.New("Rumble: paste the Live Stream API link (Account → Live Stream API on rumble.com)")
	}
	u, err := url.Parse(link)
	host := ""
	if err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if err != nil || u.Scheme != "https" || (host != "rumble.com" && !strings.HasSuffix(host, ".rumble.com")) {
		return nil, errors.New("Rumble: that isn't a Live Stream API link (it starts with https://rumble.com/)")
	}
	return &rumble{link: link, hc: defaultClient, liveEvery: 5 * time.Second, idleEvery: 30 * time.Second, seen: map[string]bool{}}, nil
}

func (r *rumble) platform() string { return Rumble }

// rumbleMoney is a number that may come as a number or as text.
type rumbleMoney struct {
	v  float64
	ok bool
}

func (m *rumbleMoney) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	var f float64
	if _, err := fmt.Sscan(s, &f); err == nil {
		m.v, m.ok = f, true
	}
	return nil
}

type rumbleChatItem struct {
	Username      string      `json:"username"`
	Text          string      `json:"text"`
	CreatedOn     string      `json:"created_on"`
	AmountDollars rumbleMoney `json:"amount_dollars"`
	AmountCents   rumbleMoney `json:"amount_cents"`
}

type rumbleAnswer struct {
	Username    string `json:"username"`
	ChannelName string `json:"channel_name"`
	Livestreams []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		IsLive      bool   `json:"is_live"`
		WatchingNow *int   `json:"watching_now"`
		Chat        struct {
			RecentMessages []rumbleChatItem `json:"recent_messages"`
			RecentRants    []rumbleChatItem `json:"recent_rants"`
		} `json:"chat"`
	} `json:"livestreams"`
}

// rumbleID is a message's id: created_on + username + text.
func rumbleID(it rumbleChatItem) string {
	h := sha1.Sum([]byte(it.CreatedOn + "\x00" + it.Username + "\x00" + it.Text))
	return Rumble + ":" + hex.EncodeToString(h[:10])
}

func (it rumbleChatItem) message() Message {
	m := Message{ID: rumbleID(it), Platform: Rumble, Author: strings.TrimSpace(it.Username), Text: it.Text}
	if t, err := time.Parse(time.RFC3339, it.CreatedOn); err == nil {
		m.At = t
	}
	if m.Author == "" {
		m.Author = "Rumble viewer"
	}
	switch {
	case it.AmountCents.ok:
		m.Super = fmt.Sprintf("$%.2f", it.AmountCents.v/100)
	case it.AmountDollars.ok:
		m.Super = fmt.Sprintf("$%.2f", it.AmountDollars.v)
	}
	return m
}

// fresh keeps the messages not seen before and remembers them.
func (r *rumble) fresh(items []rumbleChatItem, rants bool) []Message {
	var out []Message
	for _, it := range items {
		m := it.message()
		if rants && m.Super == "" {
			m.Super = "Rant"
		}
		if r.seen[m.ID] {
			continue
		}
		r.seen[m.ID] = true
		r.order = append(r.order, m.ID)
		out = append(out, m)
	}
	if over := len(r.order) - rumbleSeenMax; over > 0 {
		for _, id := range r.order[:over] {
			delete(r.seen, id)
		}
		r.order = append([]string(nil), r.order[over:]...)
	}
	return out
}

var errRumbleLink = errors.New("Rumble didn't accept the Live Stream API link (it expired or is wrong; copy it again from Account → Live Stream API)")

func (r *rumble) fetch(ctx context.Context) (rumbleAnswer, error) {
	var a rumbleAnswer
	err := getJSON(ctx, r.hc, r.link, nil, &a, func(status int, body []byte) error {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return errRumbleLink
		}
		return &apiError{Status: status}
	})
	if err != nil && !errors.Is(err, errRumbleLink) {
		err = errors.New(redact(err.Error(), r.link, rumbleKey(r.link)))
	}
	return a, err
}

// rumbleKey is the link's key, to keep it out of any text.
func rumbleKey(link string) string {
	if u, err := url.Parse(link); err == nil {
		return u.Query().Get("key")
	}
	return ""
}

func (r *rumble) run(ctx context.Context, out sink) {
	var last sourceStatus
	seen := false
	report := func(st sourceStatus) { // only when it changes (polled every 5 s)
		if !seen || st != last {
			seen, last = true, st
			out.status(st)
		}
	}
	for ctx.Err() == nil {
		a, err := r.fetch(ctx)
		if ctx.Err() != nil {
			return
		}
		label := a.ChannelName
		if label == "" {
			label = a.Username
		}
		wait := r.idleEvery
		switch {
		case err != nil:
			note := "Rumble: " + err.Error()
			if errors.Is(err, errRumbleLink) {
				note = err.Error()
			}
			report(sourceStatus{Platform: Rumble, State: stError, Note: note, Viewers: -1})
		default:
			idx := -1
			for i, l := range a.Livestreams {
				if l.IsLive {
					idx = i
					break
				}
			}
			if idx < 0 {
				report(sourceStatus{Platform: Rumble, State: stWaiting, Note: "No Rumble live stream right now", Viewers: -1, Label: label})
				break
			}
			l := a.Livestreams[idx]
			viewers := -1
			if l.WatchingNow != nil {
				viewers = *l.WatchingNow
			}
			// rants first: a rant that also shows in recent_messages keeps its amount
			msgs := append(r.fresh(l.Chat.RecentRants, true), r.fresh(l.Chat.RecentMessages, false)...)
			out.add(msgs...)
			report(sourceStatus{Platform: Rumble, State: stLive, Viewers: viewers, Title: l.Title, Label: label})
			wait = r.liveEvery
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}
