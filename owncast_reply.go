package main

// Owncast replies: over the chat websocket the connection already holds,
// as {"type":"CHAT","body":"…"}, under the "Reply as" name. No sign-in.
// Owncast has no threads, so a reply is "@author text".

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// owncastAckWait is how long a send waits to see itself in the chat.
var owncastAckWait = 4 * time.Second

type ownAck struct {
	id  string // our message, as the chat showed it
	err error  // or why the server refused it
}

// owncastReply is an Owncast chat connection's reply side.
type owncastReply struct {
	sendMu sync.Mutex // one send at a time (acks aren't matched to sends)

	mu       sync.Mutex
	ws       *wsConn
	me       string // our chat user id
	waiter   chan ownAck
	rejected string // the last refusal ("" none)
}

func (r *owncastReply) connect(ws *wsConn, me string) {
	r.mu.Lock()
	r.ws, r.me = ws, me
	r.mu.Unlock()
}

func (r *owncastReply) disconnect(ws *wsConn) {
	r.mu.Lock()
	if r.ws == ws {
		r.ws, r.me = nil, ""
	}
	r.mu.Unlock()
}

// own takes our own messages out of what the chat shows (the hub already
// has them as "YOU") and hands them, or a refusal, to a waiting send.
func (r *owncastReply) own(raw string, msgs []Message) []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.me == "" {
		return msgs
	}
	ack := func(a ownAck) {
		if r.waiter != nil {
			select {
			case r.waiter <- a:
			default:
			}
		}
	}
	mine := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		var e owncastEvent
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &e) != nil {
			continue
		}
		switch e.Type {
		case "CHAT":
			if e.User != nil && e.User.ID == r.me && e.ID != "" {
				mine[Owncast+":"+e.ID] = true
				r.rejected = ""
				ack(ownAck{id: Owncast + ":" + e.ID})
			}
		case "CHAT_ACTION":
			if why := owncastRefusal(plainText(e.Body)); why != "" && r.waiter != nil {
				r.rejected = why
				ack(ownAck{err: errors.New(why)})
			}
		}
	}
	if len(mine) == 0 {
		return msgs
	}
	out := msgs[:0:0]
	for _, m := range msgs {
		if !mine[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// owncastRefusal turns Owncast's words to us into a plain reason, or ""
// when the action isn't a refusal.
func owncastRefusal(body string) string {
	low := strings.ToLower(body)
	switch {
	case strings.Contains(low, "authentication is required"):
		return "this Owncast server only lets signed-in users chat (turn off \"require authentication\" in its chat settings, or reply from Owncast)"
	case strings.Contains(low, "established chat participant"):
		return "this Owncast server only lets established chat users post (new users wait a while)"
	case strings.Contains(low, "disabled") || strings.Contains(low, "timed out") || strings.Contains(low, "too quickly"):
		return "Owncast said: " + body
	}
	return ""
}

// canReply (replier): the chat is connected.
func (o *owncast) canReply() (bool, string, string) {
	o.r.mu.Lock()
	defer o.r.mu.Unlock()
	switch {
	case o.r.ws == nil:
		return false, o.name, "chat isn't connected"
	case strings.Contains(o.r.rejected, "signed-in users"):
		return false, o.name, "this server only lets signed-in users chat"
	}
	return true, o.name, ""
}

// send (replier): one CHAT event, then wait to see it (or the refusal).
func (o *owncast) send(ctx context.Context, text string, _ *Message) (string, error) {
	o.r.sendMu.Lock()
	defer o.r.sendMu.Unlock()
	ch := make(chan ownAck, 1)
	o.r.mu.Lock()
	ws := o.r.ws
	if ws != nil {
		o.r.waiter = ch
	}
	o.r.mu.Unlock()
	if ws == nil {
		return "", errors.New("chat isn't connected")
	}
	defer func() {
		o.r.mu.Lock()
		o.r.waiter = nil
		o.r.mu.Unlock()
	}()
	b, _ := json.Marshal(map[string]string{"type": "CHAT", "body": text})
	if err := ws.send(string(b)); err != nil {
		return "", err
	}
	t := time.NewTimer(owncastAckWait)
	defer t.Stop()
	select {
	case a := <-ch:
		return a.id, a.err
	case <-t.C:
		return "", errors.New("Owncast didn't show it in chat (is the stream live? Owncast drops chat 5 minutes after a stream ends)")
	case <-ctx.Done():
		return "", errors.New("no answer (timed out)")
	}
}

// signIn (signer): no sign-in; the field says the name replies use.
func (o *owncast) signIn(context.Context, signParams) addon.ActionFieldResult {
	ok, _, why := o.canReply()
	if ok {
		return addon.ActionFieldResult{OK: true, Text: "Replies post as \"" + o.name + "\" (no sign-in needed).", Button: "Check again"}
	}
	return addon.ActionFieldResult{Text: "Replies post as \"" + o.name + "\" once the chat is connected (now: " + why + ").", Button: "Check again"}
}
