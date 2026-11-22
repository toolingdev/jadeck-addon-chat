package main

// YouTube live chat over the YouTube Data API v3, with an API key only
// (reading a public live chat needs no sign-in).
//
// Quota (10,000 units a day by default, per Google Cloud project):
//   - finding the live stream of a channel: playlistItems.list on the
//     channel's uploads (1) + videos.list (1), every minute while nothing
//     is live (search.list costs 100, so it's only a rare fallback);
//   - liveChatMessages.list (5) every pollingIntervalMillis, but never
//     more often than the connection's floor (8 s by default);
//   - videos.list (1) a minute for the viewer count and to notice the end.
// The key goes in the X-Goog-Api-Key header, never in a URL.

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

const youtubeAPI = "https://www.googleapis.com/youtube/v3"

// YouTube target kinds.
const (
	ytVideo   = "video"
	ytChannel = "channel"
	ytHandle  = "handle"
)

var (
	reVideoID   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	reChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	reHandle    = regexp.MustCompile(`^@[A-Za-z0-9._-]{3,30}$`)
)

// parseYouTubeTarget reads the connection's "channel or live video" field:
// a channel id (UC…), a @handle, a video id, or any YouTube link to one.
func parseYouTubeTarget(s string) (kind, id string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("YouTube: enter the channel id, @handle or live video link")
	}
	switch {
	case reChannelID.MatchString(s):
		return ytChannel, s, nil
	case reHandle.MatchString(s):
		return ytHandle, s, nil
	case reVideoID.MatchString(s):
		return ytVideo, s, nil
	}
	raw := s
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, perr := url.Parse(raw)
	if perr == nil {
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		host = strings.TrimPrefix(host, "m.")
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case host == "youtu.be" && len(parts) >= 1 && reVideoID.MatchString(parts[0]):
			return ytVideo, parts[0], nil
		case host == "youtube.com" || host == "studio.youtube.com":
			if v := u.Query().Get("v"); reVideoID.MatchString(v) {
				return ytVideo, v, nil
			}
			if len(parts) >= 2 && (parts[0] == "live" || parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "video") && reVideoID.MatchString(parts[1]) {
				return ytVideo, parts[1], nil
			}
			if len(parts) >= 2 && parts[0] == "channel" && reChannelID.MatchString(parts[1]) {
				return ytChannel, parts[1], nil
			}
			if len(parts) >= 1 && reHandle.MatchString(parts[0]) {
				return ytHandle, parts[0], nil
			}
		}
	}
	return "", "", fmt.Errorf("YouTube: %q isn't a channel id (UC…), @handle, video id or YouTube link", s)
}

// youtube polls one channel's (or one video's) live chat.
type youtube struct {
	key        string
	kind, id   string
	base       string
	hc         *http.Client
	minPoll    time.Duration // floor between chat polls
	checkEvery time.Duration // how often to look for a live stream while none is
	statsEvery time.Duration // viewer count / end check while live
	searchGap  time.Duration // search.list fallback at most this often
	quotaWait  time.Duration // pause after quotaExceeded

	channel, uploads string // resolved once
	lastSearch       time.Time
}

func newYouTube(key, target string, minPoll time.Duration) (*youtube, error) {
	kind, id, err := parseYouTubeTarget(target)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("YouTube: enter the API key")
	}
	if minPoll < 3*time.Second {
		minPoll = 3 * time.Second
	}
	return &youtube{key: strings.TrimSpace(key), kind: kind, id: id, base: youtubeAPI, hc: defaultClient,
		minPoll: minPoll, checkEvery: 60 * time.Second, statsEvery: 60 * time.Second,
		searchGap: 2 * time.Hour, quotaWait: 15 * time.Minute}, nil
}

func (y *youtube) platform() string { return YouTube }

// ytErrorBody is Google's error JSON.
type ytErrorBody struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

func (y *youtube) get(ctx context.Context, path string, q url.Values, out any) error {
	hdr := http.Header{"X-Goog-Api-Key": {y.key}}
	return getJSON(ctx, y.hc, y.base+path+"?"+q.Encode(), hdr, out, func(status int, body []byte) error {
		var b ytErrorBody
		json.Unmarshal(body, &b)
		e := &apiError{Status: status, Message: redact(b.Error.Message, y.key)}
		if len(b.Error.Errors) > 0 {
			e.Reason = b.Error.Errors[0].Reason
		}
		return e
	})
}

func ytReason(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

// ytVideoItem is a videos.list item (the parts we ask for).
type ytVideoItem struct {
	ID      string `json:"id"`
	Snippet struct {
		Title                string `json:"title"`
		LiveBroadcastContent string `json:"liveBroadcastContent"` // live | upcoming | none
	} `json:"snippet"`
	Live *struct {
		ActualStartTime   string `json:"actualStartTime"`
		ActualEndTime     string `json:"actualEndTime"`
		ConcurrentViewers string `json:"concurrentViewers"` // a number, as text
		ActiveLiveChatID  string `json:"activeLiveChatId"`
	} `json:"liveStreamingDetails"`
}

// liveVideo is the stream whose chat is read.
type liveVideo struct {
	videoID, chatID, title string
	viewers                int // -1 unknown
}

func (v ytVideoItem) live() (liveVideo, bool) {
	if v.Live == nil || v.Live.ActiveLiveChatID == "" || v.Live.ActualEndTime != "" || v.Live.ActualStartTime == "" {
		return liveVideo{}, false
	}
	n, err := strconv.Atoi(v.Live.ConcurrentViewers)
	if err != nil {
		n = -1
	}
	return liveVideo{videoID: v.ID, chatID: v.Live.ActiveLiveChatID, title: v.Snippet.Title, viewers: n}, true
}

func (y *youtube) videos(ctx context.Context, ids ...string) ([]ytVideoItem, error) {
	var r struct {
		Items []ytVideoItem `json:"items"`
	}
	err := y.get(ctx, "/videos", url.Values{"part": {"snippet,liveStreamingDetails"}, "id": {strings.Join(ids, ",")}}, &r)
	return r.Items, err
}

// resolve finds the channel's uploads playlist (and the channel of a @handle).
func (y *youtube) resolve(ctx context.Context) error {
	if y.uploads != "" {
		return nil
	}
	q := url.Values{"part": {"id,contentDetails"}}
	if y.kind == ytHandle {
		q.Set("forHandle", y.id)
	} else {
		q.Set("id", y.id)
	}
	var r struct {
		Items []struct {
			ID             string `json:"id"`
			ContentDetails struct {
				RelatedPlaylists struct {
					Uploads string `json:"uploads"`
				} `json:"relatedPlaylists"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := y.get(ctx, "/channels", q, &r); err != nil {
		return err
	}
	if len(r.Items) == 0 {
		return fmt.Errorf("YouTube has no channel %s", y.id)
	}
	y.channel, y.uploads = r.Items[0].ID, r.Items[0].ContentDetails.RelatedPlaylists.Uploads
	return nil
}

// findLive finds the live stream with an active chat, or errNotLive.
func (y *youtube) findLive(ctx context.Context) (liveVideo, error) {
	if y.kind == ytVideo {
		items, err := y.videos(ctx, y.id)
		if err != nil {
			return liveVideo{}, err
		}
		if len(items) == 0 {
			return liveVideo{}, fmt.Errorf("YouTube has no video %s (or it's private)", y.id)
		}
		if lv, ok := items[0].live(); ok {
			return lv, nil
		}
		return liveVideo{}, errNotLive
	}
	if err := y.resolve(ctx); err != nil {
		return liveVideo{}, err
	}
	// A stream that's live is among the newest uploads (2 units).
	var pl struct {
		Items []struct {
			ContentDetails struct {
				VideoID string `json:"videoId"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if y.uploads != "" {
		if err := y.get(ctx, "/playlistItems", url.Values{"part": {"contentDetails"}, "playlistId": {y.uploads}, "maxResults": {"10"}}, &pl); err != nil && ytReason(err) != "playlistNotFound" {
			return liveVideo{}, err
		}
	}
	var ids []string
	for _, it := range pl.Items {
		if it.ContentDetails.VideoID != "" {
			ids = append(ids, it.ContentDetails.VideoID)
		}
	}
	if len(ids) > 0 {
		items, err := y.videos(ctx, ids...)
		if err != nil {
			return liveVideo{}, err
		}
		for _, it := range items {
			if lv, ok := it.live(); ok {
				return lv, nil
			}
		}
	}
	// Rarely: ask search (100 units) in case the uploads list missed it.
	if time.Since(y.lastSearch) < y.searchGap {
		return liveVideo{}, errNotLive
	}
	y.lastSearch = time.Now()
	var sr struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
		} `json:"items"`
	}
	if err := y.get(ctx, "/search", url.Values{"part": {"id"}, "channelId": {y.channel}, "eventType": {"live"}, "type": {"video"}, "maxResults": {"1"}}, &sr); err != nil {
		return liveVideo{}, err
	}
	if len(sr.Items) == 0 || sr.Items[0].ID.VideoID == "" {
		return liveVideo{}, errNotLive
	}
	items, err := y.videos(ctx, sr.Items[0].ID.VideoID)
	if err != nil {
		return liveVideo{}, err
	}
	for _, it := range items {
		if lv, ok := it.live(); ok {
			return lv, nil
		}
	}
	return liveVideo{}, errNotLive
}

// chatPage is one liveChatMessages.list answer.
type chatPage struct {
	msgs     []Message
	deleted  []string // ids to remove
	next     string
	interval time.Duration
	ended    bool
}

type ytChatResponse struct {
	NextPageToken         string `json:"nextPageToken"`
	PollingIntervalMillis int    `json:"pollingIntervalMillis"`
	OfflineAt             string `json:"offlineAt"`
	Items                 []struct {
		ID      string `json:"id"`
		Snippet struct {
			Type               string `json:"type"`
			PublishedAt        string `json:"publishedAt"`
			DisplayMessage     string `json:"displayMessage"`
			TextMessageDetails struct {
				MessageText string `json:"messageText"`
			} `json:"textMessageDetails"`
			SuperChatDetails struct {
				AmountDisplayString string `json:"amountDisplayString"`
				UserComment         string `json:"userComment"`
			} `json:"superChatDetails"`
			SuperStickerDetails struct {
				AmountDisplayString string `json:"amountDisplayString"`
			} `json:"superStickerDetails"`
			MemberMilestoneChatDetails struct {
				UserComment string `json:"userComment"`
			} `json:"memberMilestoneChatDetails"`
			MessageDeletedDetails struct {
				DeletedMessageID string `json:"deletedMessageId"`
			} `json:"messageDeletedDetails"`
		} `json:"snippet"`
		AuthorDetails struct {
			DisplayName     string `json:"displayName"`
			ProfileImageURL string `json:"profileImageUrl"`
		} `json:"authorDetails"`
	} `json:"items"`
}

// parseYouTubeChat turns a liveChatMessages.list answer into messages.
func parseYouTubeChat(r ytChatResponse) chatPage {
	p := chatPage{next: r.NextPageToken, interval: time.Duration(r.PollingIntervalMillis) * time.Millisecond, ended: r.OfflineAt != ""}
	for _, it := range r.Items {
		s := it.Snippet
		m := Message{ID: YouTube + ":" + it.ID, Platform: YouTube, Author: strings.TrimSpace(it.AuthorDetails.DisplayName), Avatar: it.AuthorDetails.ProfileImageURL}
		m.At, _ = time.Parse(time.RFC3339Nano, s.PublishedAt)
		switch s.Type {
		case "textMessageEvent":
			m.Text = s.TextMessageDetails.MessageText
		case "superChatEvent":
			m.Super, m.Text = s.SuperChatDetails.AmountDisplayString, s.SuperChatDetails.UserComment
		case "superStickerEvent":
			m.Super, m.Text = s.SuperStickerDetails.AmountDisplayString, "Super Sticker"
		case "memberMilestoneChatEvent":
			m.Text = s.MemberMilestoneChatDetails.UserComment
		case "newSponsorEvent", "membershipGiftingEvent":
			m.Text = s.DisplayMessage
		case "messageDeletedEvent":
			if id := s.MessageDeletedDetails.DeletedMessageID; id != "" {
				p.deleted = append(p.deleted, YouTube+":"+id)
			}
			continue
		case "chatEndedEvent":
			p.ended = true
			continue
		default:
			continue
		}
		if m.Text == "" && m.Super == "" {
			m.Text = s.DisplayMessage
		}
		if m.Author == "" {
			m.Author = "YouTube viewer"
		}
		p.msgs = append(p.msgs, m)
	}
	return p
}

func (y *youtube) pollChat(ctx context.Context, chatID, page string) (chatPage, error) {
	q := url.Values{"liveChatId": {chatID}, "part": {"snippet,authorDetails"}, "maxResults": {"200"}}
	if page != "" {
		q.Set("pageToken", page)
	}
	var r ytChatResponse
	if err := y.get(ctx, "/liveChat/messages", q, &r); err != nil {
		switch ytReason(err) {
		case "liveChatEnded", "liveChatNotFound", "liveChatDisabled":
			return chatPage{ended: true}, nil
		}
		return chatPage{}, err
	}
	return parseYouTubeChat(r), nil
}

// problem turns an error into the note the widget shows.
func (y *youtube) problem(err error) string {
	switch ytReason(err) {
	case "quotaExceeded", "dailyLimitExceeded", "rateLimitExceeded":
		return "YouTube's daily API quota is used up; trying again later"
	case "keyInvalid", "badRequest":
		return "YouTube didn't accept the API key"
	case "accessNotConfigured", "SERVICE_DISABLED":
		return "Turn on the YouTube Data API v3 for this key's Google Cloud project"
	case "ipRefererBlocked", "forbidden":
		return "YouTube refused this key here (check its restrictions)"
	}
	return "YouTube: " + err.Error()
}

func (y *youtube) run(ctx context.Context, out sink) {
	st := sourceStatus{Platform: YouTube, Viewers: -1}
	for ctx.Err() == nil {
		lv, err := y.findLive(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			st = sourceStatus{Platform: YouTube, State: stWaiting, Note: "No YouTube live stream right now", Viewers: -1}
			wait := y.checkEvery
			if !errors.Is(err, errNotLive) {
				st.State, st.Note = stError, y.problem(err)
				if r := ytReason(err); r == "quotaExceeded" || r == "dailyLimitExceeded" {
					wait = y.quotaWait
				}
			}
			out.status(st)
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		st = sourceStatus{Platform: YouTube, State: stLive, Viewers: lv.viewers, Title: lv.title}
		out.status(st)
		y.follow(ctx, lv, out, st)
	}
}

// follow polls one live chat until it ends.
func (y *youtube) follow(ctx context.Context, lv liveVideo, out sink, st sourceStatus) {
	page, fails, stats := "", 0, time.Now()
	for ctx.Err() == nil {
		p, err := y.pollChat(ctx, lv.chatID, page)
		if ctx.Err() != nil {
			return
		}
		wait := y.minPoll
		if err != nil {
			fails++
			st.Note = y.problem(err)
			out.status(st)
			if r := ytReason(err); r == "quotaExceeded" || r == "dailyLimitExceeded" {
				st.State = stError
				out.status(st)
				sleep(ctx, y.quotaWait)
				return
			}
			wait = min(y.minPoll*time.Duration(1<<min(fails, 4)), 2*time.Minute)
		} else {
			if fails > 0 || st.Note != "" {
				fails, st.Note = 0, ""
				out.status(st)
			}
			out.add(p.msgs...)
			for _, id := range p.deleted {
				out.remove(id)
			}
			if p.ended {
				return
			}
			page = p.next
			wait = max(wait, p.interval)
		}
		if time.Since(stats) >= y.statsEvery {
			stats = time.Now()
			if items, err := y.videos(ctx, lv.videoID); err == nil {
				cur, ok := ytVideoItem{}, len(items) > 0
				if ok {
					cur = items[0]
				}
				now, live := cur.live()
				if !live {
					return
				}
				st.Viewers = now.viewers
				out.status(st)
			}
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}
