package main

// Replies: a message to everyone (every platform that can take one) or a
// reply to one comment (only its platform). The hub routes send_message
// to each platform feed's replier, puts "YOU" in the chat at once with a
// ✓ / ✕ per platform, keeps the platforms' echoes of it out (by id, and
// by account + text when a platform's id differs), and remembers the
// last send for the widget's result card and retry_send.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/toolingdev/jadeck/sdk/addon"
	"slices"
)

// replier is what a platform feed does to reply. canReply must be quick
// (no network): it reads what the platform last learned.
type replier interface {
	canReply() (ok bool, account, why string)
	// send posts text (already shaped for the platform) and returns the
	// platform's id of the new message (as the chat shows it,
	// "<platform>:<id>"), or "" when it can't know.
	send(ctx context.Context, text string, replyTo *Message) (string, error)
}

// signer is a platform with a sign-in action field (reply_signin).
type signer interface {
	signIn(ctx context.Context, p signParams) addon.ActionFieldResult
}

// maxLen is the longest message each platform takes, in characters.
//   - YouTube: 200 (the live chat box's limit; the API docs give none).
//   - Twitch: 500 (Send Chat Message).
//   - Facebook and Owncast: 500 (their own limits are larger).
var maxLen = map[string]int{YouTube: 200, Twitch: 500, Facebook: 500, Owncast: 500}

// sendEvery: at most one send a second per platform connection.
var sendEvery = time.Second

// sendGate spaces one feed's sends.
type sendGate struct {
	mu   sync.Mutex
	last time.Time
}

// wait holds until a second has passed since the last send.
func (g *sendGate) wait(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d := sendEvery - time.Since(g.last); d > 0 {
		if !sleep(ctx, d) {
			return ctx.Err()
		}
	}
	g.last = time.Now()
	return nil
}

// sendResult is one platform's answer, as the result card shows it.
type sendResult struct {
	Feed     string `json:"feed"`
	Platform string `json:"platform"`
	Label    string `json:"label"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`

	text string // what went (or would go) to this platform
}

// sendRecord is one send_message.
type sendRecord struct {
	ID      string       `json:"id"`
	Text    string       `json:"text"`
	ReplyTo string       `json:"replyTo,omitempty"` // the author answered
	Results []sendResult `json:"results"`
	Sent    int          `json:"sent"`
	Of      int          `json:"of"`

	msgID   string
	replyTo *Message
}

func (r *sendRecord) tally() {
	r.Sent, r.Of = 0, len(r.Results)
	for _, x := range r.Results {
		if x.OK {
			r.Sent++
		}
	}
}

func (r *sendRecord) marks() []sentMark {
	out := make([]sentMark, 0, len(r.Results))
	for _, x := range r.Results {
		out = append(out, sentMark{Platform: x.Platform, OK: x.OK})
	}
	return out
}

// echo is a sent message a platform will show again.
type echo struct {
	plat, account, text string
	until               time.Time
}

// shaped is the text for one platform: platforms without threads get
// "@author text" for a reply.
func shaped(plat, text string, replyTo *Message) string {
	if replyTo != nil && (plat == YouTube || plat == Owncast) {
		return "@" + replyTo.Author + " " + text
	}
	return text
}

func normText(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// dropEchoes takes out the platforms' copies of messages sent from here.
func (c *Conn) dropEchoes(msgs []Message) []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.echoes) == 0 {
		return msgs
	}
	now := time.Now()
	live := c.echoes[:0]
	for _, e := range c.echoes {
		if now.Before(e.until) {
			live = append(live, e)
		}
	}
	c.echoes = live
	out := msgs[:0:0]
next:
	for _, m := range msgs {
		for i, e := range c.echoes {
			if m.Platform == e.plat && normText(m.Text) == normText(e.text) && (e.account == "" || strings.EqualFold(m.Author, e.account)) {
				c.echoes = append(c.echoes[:i], c.echoes[i+1:]...)
				continue next
			}
		}
		out = append(out, m)
	}
	return out
}

// replyTargets is every open platform feed (one per chip) with whether
// it can reply now.
type target struct {
	v       feedView
	ok      bool
	account string
	why     string
}

func (c *Conn) targets() []target {
	c.mu.Lock()
	vs := c.chips()
	c.mu.Unlock()
	out := make([]target, 0, len(vs))
	for _, v := range vs {
		t := target{v: v, why: "read only"}
		if v.rep != nil {
			t.ok, t.account, t.why = v.rep.canReply()
		}
		out = append(out, t)
	}
	return out
}

func (t target) label() string { return platformNames[t.v.plat] }

// sendParams is send_message's params.
type sendParams struct {
	Text      string `json:"text"`
	Platforms any    `json:"platforms"` // "youtube,twitch-3" or a list
	ReplyTo   string `json:"replyTo"`
}

func (p sendParams) wanted() []string {
	var raw []string
	switch v := p.Platforms.(type) {
	case string:
		raw = strings.Split(v, ",")
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	var out []string
	for _, s := range raw {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var sendSeq struct {
	sync.Mutex
	n int
}

func nextSendID() string {
	sendSeq.Lock()
	defer sendSeq.Unlock()
	sendSeq.n++
	return fmt.Sprintf("s%d-%s", sendSeq.n, strconv.FormatInt(time.Now().UnixMilli()%1e8, 36))
}

// sendMessage is the send_message action.
func (c *Conn) sendMessage(ctx context.Context, raw json.RawMessage) (any, error) {
	var p sendParams
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("unreadable params: %v", err)
		}
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return nil, errors.New("type a message first")
	}
	all := c.targets()
	if len(all) == 0 {
		return nil, errors.New("no platform chat connection is open")
	}
	rec := &sendRecord{ID: nextSendID(), Text: text}
	var picked []target
	if p.ReplyTo != "" {
		m, ok := c.store.get(p.ReplyTo)
		if !ok {
			return nil, errors.New("that message is no longer in the chat")
		}
		if m.Mine {
			return nil, errors.New("that's a message sent from here")
		}
		var best *target
		for i, t := range all {
			if t.v.plat != m.Platform {
				continue
			}
			if t.v.id == m.feed || best == nil || (!best.ok && t.ok) {
				best = &all[i]
			}
			if t.v.id == m.feed {
				break
			}
		}
		if best == nil {
			return nil, fmt.Errorf("%s isn't connected any more", platformNames[m.Platform])
		}
		if !best.ok {
			return nil, fmt.Errorf("%s can't reply: %s", best.label(), best.why)
		}
		picked = []target{*best}
		rec.replyTo, rec.ReplyTo = &m, m.Author
	} else if want := p.wanted(); len(want) > 0 {
		for _, t := range all {
			for _, w := range want {
				if w == t.v.id || w == t.v.plat || w == strings.ToLower(t.label()) {
					picked = append(picked, t)
					break
				}
			}
		}
		if len(picked) == 0 {
			return nil, fmt.Errorf("no open platform matches %q", strings.Join(want, ","))
		}
	} else {
		for _, t := range all {
			if t.ok {
				picked = append(picked, t)
			}
		}
		if len(picked) == 0 {
			return nil, errors.New("no platform can take replies right now (sign in under Settings → Connections)")
		}
	}

	account := ""
	for _, t := range picked {
		r := sendResult{Feed: t.v.id, Platform: t.v.plat, Label: t.v.label, text: shaped(t.v.plat, text, rec.replyTo)}
		if !t.ok {
			r.Error = t.label() + ": " + t.why
		} else if n, lim := utf8.RuneCountInString(r.text), maxLen[t.v.plat]; lim > 0 && n > lim {
			r.Error = fmt.Sprintf("%s takes at most %d characters (this is %d)", t.label(), lim, n)
		}
		if account == "" && t.ok {
			account = t.account
		}
		rec.Results = append(rec.Results, r)
	}
	if account == "" {
		account = "jadeck"
	}

	// In the chat right away, before any platform answers.
	rec.msgID = "you:" + rec.ID
	rec.tally()
	c.store.add(Message{ID: rec.msgID, Platform: "you", Author: account, Text: text, At: time.Now(), Mine: true, ReplyTo: rec.ReplyTo})
	c.changed()

	c.deliver(ctx, rec, func(sendResult) bool { return true })

	c.mu.Lock()
	c.sends[rec.ID] = rec
	c.sendOrder = append(c.sendOrder, rec.ID)
	if over := len(c.sendOrder) - 20; over > 0 {
		for _, id := range c.sendOrder[:over] {
			delete(c.sends, id)
		}
		c.sendOrder = append([]string(nil), c.sendOrder[over:]...)
	}
	c.lastSend = rec
	defer c.mu.Unlock()
	return rec.result(), nil
}

// deliver sends rec to the results that pick() chooses and have no
// standing problem, all at once, and updates the chat's ✓ / ✕.
func (c *Conn) deliver(ctx context.Context, rec *sendRecord, pick func(sendResult) bool) {
	c.mu.Lock()
	feeds := map[string]feedView{}
	for _, v := range c.feeds {
		feeds[v.id] = v
	}
	chips := c.chips()
	c.mu.Unlock()
	find := func(r sendResult) (feedView, bool) {
		if v, ok := feeds[r.Feed]; ok {
			return v, true
		}
		for _, v := range chips { // reopened since: the same platform and label
			if v.plat == r.Platform && strings.EqualFold(v.label, r.Label) {
				return v, true
			}
		}
		return feedView{}, false
	}
	c.mu.Lock()
	results := slices.Clone(rec.Results)
	replyTo := rec.replyTo
	c.mu.Unlock()
	var wg sync.WaitGroup
	for i := range results {
		r := &results[i]
		if !pick(*r) {
			continue
		}
		if strings.Contains(r.Error, "takes at most") {
			continue
		}
		name := platformNames[r.Platform]
		v, found := find(*r)
		if !found {
			r.OK, r.Error = false, name+" isn't connected any more"
			continue
		}
		if v.rep == nil {
			r.OK, r.Error = false, name+": read only"
			continue
		}
		ok, account, why := v.rep.canReply()
		if !ok {
			r.OK, r.Error = false, name+": "+why
			continue
		}
		wg.Add(1)
		go func(v feedView) {
			defer wg.Done()
			r.Error = ""
			if err := v.gate.wait(ctx); err != nil {
				r.OK, r.Error = false, name+": no answer (timed out)"
				return
			}
			id, err := v.rep.send(ctx, r.text, replyTo)
			if err != nil {
				r.OK, r.Error = false, plainSendError(r.Platform, err)
				return
			}
			r.OK = true
			c.store.suppress(id)
			c.mu.Lock()
			c.echoes = append(c.echoes, echo{plat: r.Platform, account: account, text: r.text, until: time.Now().Add(2 * time.Minute)})
			c.mu.Unlock()
		}(v)
	}
	wg.Wait()
	c.mu.Lock()
	rec.Results = results
	rec.tally()
	marks := rec.marks()
	c.mu.Unlock()
	c.store.setSent(rec.msgID, marks)
	c.changed()
}

// plainSendError is a platform's failure in plain words, with its name.
func plainSendError(plat string, err error) string {
	name := platformNames[plat]
	msg := err.Error()
	if strings.HasPrefix(msg, name) {
		return msg
	}
	return name + ": " + msg
}

func (r *sendRecord) result() map[string]any {
	res := make([]map[string]any, 0, len(r.Results))
	for _, x := range r.Results {
		m := map[string]any{"feed": x.Feed, "platform": x.Platform, "label": x.Label, "ok": x.OK}
		if x.Error != "" {
			m["error"] = x.Error
		}
		res = append(res, m)
	}
	return map[string]any{"ok": r.Sent == r.Of, "id": r.ID, "sent": r.Sent, "of": r.Of, "results": res}
}

// retrySend is retry_send: only the platforms that failed, again.
func (c *Conn) retrySend(ctx context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		SendID string `json:"sendId"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("unreadable params: %v", err)
		}
	}
	c.mu.Lock()
	rec := c.sends[p.SendID]
	if p.SendID == "" {
		rec = c.lastSend
	}
	c.mu.Unlock()
	if rec == nil {
		return nil, errors.New("that send is no longer here (send it again)")
	}
	c.mu.Lock()
	failed := slices.ContainsFunc(rec.Results, func(r sendResult) bool { return !r.OK })
	c.mu.Unlock()
	if failed {
		c.deliver(ctx, rec, func(r sendResult) bool { return !r.OK })
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSend = rec
	return rec.result(), nil
}

// lastSendState is the result card (nil once dismissed); c.mu held.
func (c *Conn) lastSendState() any {
	rec := c.lastSend
	if rec == nil {
		return nil
	}
	res := make([]map[string]any, 0, len(rec.Results))
	for _, x := range rec.Results {
		m := map[string]any{"platform": x.Platform, "label": x.Label, "ok": x.OK}
		if x.Error != "" {
			m["error"] = x.Error
		}
		res = append(res, m)
	}
	return map[string]any{"id": rec.ID, "text": rec.Text, "replyTo": rec.ReplyTo, "sent": rec.Sent, "of": rec.Of, "results": res}
}

// ------------------------------------------------- the sign-in action

// AddonAction (platform connections): the action field's reply_signin.
func (p *platformConn) AddonAction(ctx context.Context, name string, params json.RawMessage) (any, error) {
	s, ok := p.src.(signer)
	if name != "reply_signin" || !ok {
		return nil, fmt.Errorf("%w: %s", addon.ErrUnknownAction, name)
	}
	var sp signParams
	if len(params) > 0 && string(params) != "null" {
		json.Unmarshal(params, &sp)
	}
	r := s.signIn(ctx, sp)
	reg.touch(p.f) // canReply may have changed: redraw the widgets
	return asMap(r), nil
}
