package main

// Platform connections: one connection type per streaming platform
// (youtube, facebook, twitch, rumble, owncast). Each builds its source,
// registers it with the in-process hub (registry.go) and runs it until
// the connection is closed. They have no widgets: their Test line is
// that platform's status, and every Live chat connection merges them.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var platformNames = map[string]string{
	YouTube: "YouTube", Facebook: "Facebook", Twitch: "Twitch", Rumble: "Rumble", Owncast: "Owncast", "sample": "Sample chat",
}

// describe is one feed's status in plain words, without the platform name.
func describe(v feedView) string {
	if !v.seen {
		return "checking…"
	}
	st := v.st
	switch st.State {
	case stLive:
		d := "live"
		if st.Viewers >= 0 {
			d += fmt.Sprintf(" (%d watching)", st.Viewers)
		}
		if st.Note != "" {
			if st.Info {
				return st.Note
			}
			d += " · " + st.Note
		}
		return d
	default:
		if st.Note != "" {
			return st.Note
		}
		return st.State
	}
}

// detailLine is one feed as a Test line: "Twitch (yourchurch): chat connected …".
func detailLine(v feedView) string {
	name := platformNames[v.plat]
	if v.label != "" {
		name += " (" + v.label + ")"
	}
	return name + ": " + describe(v)
}

// platformConn is one platform connection.
type platformConn struct {
	f      *feed
	cancel context.CancelFunc
	done   chan struct{}
}

// openPlatform registers src's feed and starts it.
func openPlatform(src source, label string) *platformConn {
	f := newFeed(src.platform(), label)
	reg.register(f)
	ctx, cancel := context.WithCancel(context.Background())
	p := &platformConn{f: f, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		src.run(ctx, f)
	}()
	return p
}

// AddonState is the platform's status (the connection's Test line is
// "detail"). The first look can take a few seconds.
func (p *platformConn) AddonState(ctx context.Context) (map[string]any, error) {
	t := time.NewTimer(6 * time.Second)
	select {
	case <-p.f.ready:
	case <-t.C:
	case <-ctx.Done():
	}
	t.Stop()
	v := p.f.current()
	state := v.st.State
	if !v.seen {
		state = stWaiting
	}
	// A setup or platform problem fails Test (red, with the reason) and
	// shows the connection as not connected in Settings.
	if state == stError {
		return nil, errors.New(detailLine(v))
	}
	return map[string]any{
		"detail": detailLine(v), "platform": v.plat, "label": v.label,
		"state": state, "note": v.st.Note, "viewers": v.st.Viewers,
	}, nil
}

// Close stops the source and takes it out of every Live chat (their
// messages so far stay).
func (p *platformConn) Close() error {
	p.cancel()
	<-p.done
	reg.unregister(p.f)
	return nil
}

// ------------------------------------------------------------- openers

func readConfig(raw json.RawMessage, into any) error {
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			return fmt.Errorf("unreadable settings: %v", err)
		}
	}
	return nil
}

// youtubeConfig is a YouTube chat connection's settings.
type youtubeConfig struct {
	Key    string `json:"youtubeKey"`
	Target string `json:"youtubeChannel"`
	Poll   any    `json:"youtubePollSeconds"` // number or text
}

func (c youtubeConfig) pollFloor() time.Duration {
	secs := 8.0
	switch v := c.Poll.(type) {
	case float64:
		secs = v
	case string:
		if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			secs = x
		}
	}
	if secs < 3 {
		secs = 3
	}
	return time.Duration(secs * float64(time.Second))
}

func openYouTube(raw json.RawMessage) (any, error) {
	var c youtubeConfig
	if err := readConfig(raw, &c); err != nil {
		return nil, err
	}
	y, err := newYouTube(c.Key, c.Target, c.pollFloor())
	if err != nil {
		return nil, err
	}
	return openPlatform(y, y.id), nil
}

func openFacebook(raw json.RawMessage) (any, error) {
	var c struct {
		Page  string `json:"facebookPage"`
		Token string `json:"facebookToken"`
	}
	if err := readConfig(raw, &c); err != nil {
		return nil, err
	}
	fb, err := newFacebook(c.Page, c.Token)
	if err != nil {
		return nil, err
	}
	return openPlatform(fb, fb.page), nil
}

func openTwitch(raw json.RawMessage) (any, error) {
	var c struct {
		Channel string `json:"channel"`
	}
	if err := readConfig(raw, &c); err != nil {
		return nil, err
	}
	tw, err := newTwitch(c.Channel)
	if err != nil {
		return nil, err
	}
	return openPlatform(tw, tw.channel), nil
}

func openRumble(raw json.RawMessage) (any, error) {
	var c struct {
		URL string `json:"apiUrl"`
	}
	if err := readConfig(raw, &c); err != nil {
		return nil, err
	}
	rb, err := newRumble(c.URL)
	if err != nil {
		return nil, err
	}
	return openPlatform(rb, ""), nil
}

func openOwncast(raw json.RawMessage) (any, error) {
	var c struct {
		Server string `json:"server"`
	}
	if err := readConfig(raw, &c); err != nil {
		return nil, err
	}
	oc, err := newOwncast(c.Server)
	if err != nil {
		return nil, err
	}
	return openPlatform(oc, oc.label()), nil
}
