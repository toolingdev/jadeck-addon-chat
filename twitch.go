package main

// Twitch chat, read anonymously: IRC over a secure websocket
// (wss://irc-ws.chat.twitch.tv:443) as "justinfan<digits>", Twitch's
// read-only guest login. Public chat needs no key and no sign-in.
//
//   CAP REQ :twitch.tv/tags twitch.tv/commands
//   PASS SCHMOOPIIE
//   NICK justinfan12345
//   JOIN #channel
//
// PRIVMSG is a chat message (bits = a cheer), USERNOTICE a sub / resub /
// gifted sub, CLEARMSG and CLEARCHAT take messages away, RECONNECT asks
// us to come back. Viewer counts and live / offline need the Helix API
// with a sign-in, so "live" here means the chat is connected.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const twitchIRC = "wss://irc-ws.chat.twitch.tv:443"

var reTwitchChannel = regexp.MustCompile(`^[a-zA-Z0-9_]{3,25}$`)

// parseTwitchChannel reads "yourchurch", "#yourchurch", "@yourchurch" or
// a pasted twitch.tv link.
func parseTwitchChannel(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("Twitch: enter the channel name (from twitch.tv/<name>)")
	}
	name := s
	if low := strings.ToLower(s); strings.Contains(low, "twitch.tv") {
		raw := s
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		if u, err := url.Parse(raw); err == nil {
			host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
			host = strings.TrimPrefix(host, "m.")
			if host == "twitch.tv" {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				name = parts[0]
				if name == "popout" && len(parts) > 1 {
					name = parts[1]
				}
			}
		}
	}
	name = strings.TrimLeft(name, "#@")
	if !reTwitchChannel.MatchString(name) {
		return "", fmt.Errorf("Twitch: %q isn't a channel name (3-25 letters, digits or _, as in twitch.tv/yourchurch)", s)
	}
	return strings.ToLower(name), nil
}

type twitch struct {
	channel    string
	url        string
	minBackoff time.Duration
	maxBackoff time.Duration
}

func newTwitch(channel string) (*twitch, error) {
	ch, err := parseTwitchChannel(channel)
	if err != nil {
		return nil, err
	}
	return &twitch{channel: ch, url: twitchIRC, minBackoff: time.Second, maxBackoff: 60 * time.Second}, nil
}

func (t *twitch) platform() string { return Twitch }

// twitchNote is the status note while the chat is connected.
const twitchNote = "chat connected (Twitch doesn't share viewer counts without sign-in)"

var errTwitchReconnect = errors.New("Twitch asked to reconnect")

func (t *twitch) run(ctx context.Context, out sink) {
	backoff := t.minBackoff
	for ctx.Err() == nil {
		joined, err := t.session(ctx, out)
		if ctx.Err() != nil {
			return
		}
		if joined {
			backoff = t.minBackoff
		}
		if errors.Is(err, errTwitchReconnect) {
			continue
		}
		note := "Twitch chat: " + err.Error()
		var te *twitchNotice
		if errors.As(err, &te) {
			note = "Twitch said: " + te.msg
		}
		out.status(sourceStatus{Platform: Twitch, State: stError, Viewers: -1,
			Note: fmt.Sprintf("%s; trying again in %s", note, backoff.Round(time.Second))})
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, t.maxBackoff)
	}
}

// twitchNotice is a NOTICE that ends the session (a banned or unknown channel).
type twitchNotice struct{ msg string }

func (e *twitchNotice) Error() string { return e.msg }

// session is one connection: it returns why it ended, and whether the
// channel was joined.
func (t *twitch) session(ctx context.Context, out sink) (joined bool, err error) {
	ws, err := wsDial(ctx, t.url, nil)
	if err != nil {
		return false, err
	}
	defer ws.Close()
	nick := fmt.Sprintf("justinfan%05d", rand.IntN(100000))
	for _, line := range []string{"CAP REQ :twitch.tv/tags twitch.tv/commands", "PASS SCHMOOPIIE", "NICK " + nick, "JOIN #" + t.channel} {
		if err := ws.send(line + "\r\n"); err != nil {
			return false, err
		}
	}
	byUser := map[string][]string{} // user-id → their message ids (CLEARCHAT)
	for {
		raw, err := ws.read()
		if err != nil {
			return joined, err
		}
		for _, line := range strings.Split(raw, "\r\n") {
			if line == "" {
				continue
			}
			m := parseIRC(line)
			switch m.command {
			case "PING":
				if err := ws.send("PONG :" + m.trailing() + "\r\n"); err != nil {
					return joined, err
				}
			case "RECONNECT":
				return joined, errTwitchReconnect
			case "ROOMSTATE", "JOIN", "366":
				if m.command == "JOIN" && !strings.HasPrefix(m.prefix, nick+"!") {
					continue
				}
				if !joined {
					joined = true
					out.status(sourceStatus{Platform: Twitch, State: stLive, Viewers: -1, Note: twitchNote, Info: true})
				}
			case "NOTICE":
				switch m.tags["msg-id"] {
				case "msg_channel_suspended", "msg_banned", "msg_room_not_found", "tos_ban":
					return joined, &twitchNotice{msg: m.trailing()}
				}
				if strings.Contains(m.trailing(), "Login authentication failed") || strings.Contains(m.trailing(), "Improperly formatted auth") {
					return joined, &twitchNotice{msg: m.trailing()}
				}
			case "PRIVMSG", "USERNOTICE":
				if msg, ok := twitchMessage(m); ok {
					if uid := m.tags["user-id"]; uid != "" {
						if len(byUser) > 5000 {
							clear(byUser)
						}
						ids := append(byUser[uid], msg.ID)
						if len(ids) > 50 {
							ids = ids[len(ids)-50:]
						}
						byUser[uid] = ids
					}
					out.add(msg)
				}
			case "CLEARMSG":
				if id := m.tags["target-msg-id"]; id != "" {
					out.remove(Twitch + ":" + id)
				}
			case "CLEARCHAT":
				// One person timed out or banned: their messages go. A clear
				// of the whole chat (no user) keeps the history here.
				if m.trailing() == "" {
					continue
				}
				uid := m.tags["target-user-id"]
				for _, id := range byUser[uid] {
					out.remove(id)
				}
				delete(byUser, uid)
			}
		}
	}
}

// twitchMessage turns a PRIVMSG or USERNOTICE into a chat message.
func twitchMessage(m ircMessage) (Message, bool) {
	id := m.tags["id"]
	if id == "" {
		return Message{}, false
	}
	author := m.tags["display-name"]
	if author == "" {
		author = m.tags["login"]
	}
	if author == "" {
		author, _, _ = strings.Cut(m.prefix, "!")
	}
	if author == "" {
		author = "Twitch viewer"
	}
	msg := Message{ID: Twitch + ":" + id, Platform: Twitch, Author: author, Text: m.trailing()}
	if ms, err := strconv.ParseInt(m.tags["tmi-sent-ts"], 10, 64); err == nil && ms > 0 {
		msg.At = time.UnixMilli(ms)
	}
	if m.command == "USERNOTICE" {
		switch m.tags["msg-id"] {
		case "sub", "resub", "subgift", "submysterygift", "giftpaidupgrade", "anongiftpaidupgrade":
		default:
			return Message{}, false
		}
		msg.Super = "Sub"
		if msg.Text == "" {
			msg.Text = m.tags["system-msg"]
		}
		return msg, true
	}
	// /me: \x01ACTION waves\x01
	if t, ok := strings.CutPrefix(msg.Text, "\x01ACTION "); ok {
		msg.Text = strings.TrimSuffix(t, "\x01")
	}
	if b, err := strconv.Atoi(m.tags["bits"]); err == nil && b > 0 {
		msg.Super = fmt.Sprintf("%d bits", b)
	}
	return msg, true
}

// ircMessage is one IRC line: @tags :prefix COMMAND params :trailing.
type ircMessage struct {
	tags    map[string]string
	prefix  string
	command string
	params  []string // the last is the trailing part, when there is one
}

func (m ircMessage) trailing() string {
	if len(m.params) == 0 {
		return ""
	}
	return m.params[len(m.params)-1]
}

func parseIRC(line string) ircMessage {
	m := ircMessage{tags: map[string]string{}}
	if strings.HasPrefix(line, "@") {
		var tags string
		tags, line, _ = strings.Cut(line[1:], " ")
		for _, kv := range strings.Split(tags, ";") {
			k, v, _ := strings.Cut(kv, "=")
			m.tags[k] = unescapeTag(v)
		}
	}
	line = strings.TrimLeft(line, " ")
	if strings.HasPrefix(line, ":") {
		m.prefix, line, _ = strings.Cut(line[1:], " ")
	}
	line = strings.TrimLeft(line, " ")
	var rest string
	m.command, rest, _ = strings.Cut(line, " ")
	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		if strings.HasPrefix(rest, ":") {
			m.params = append(m.params, rest[1:])
			break
		}
		var p string
		p, rest, _ = strings.Cut(rest, " ")
		if p != "" {
			m.params = append(m.params, p)
		}
	}
	return m
}

// unescapeTag undoes IRCv3 tag escaping.
func unescapeTag(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i == len(v)-1 {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case ':':
			b.WriteByte(';')
		case '\\':
			b.WriteByte('\\')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		default:
			b.WriteByte(v[i])
		}
	}
	return b.String()
}
