// Live chat: a jadeck add-on. YouTube, Facebook, Twitch, Rumble and
// Owncast chat, each its own connection, merged in two widgets: Live
// chat (just the comments, big) and Live chat + reply (reply to one
// comment or message every platform at once, and put a message on screen
// through the user's vMix connection).
package main

import (
	"encoding/json"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// Version is set by the release build.
var Version = "0.2.2"

func main() { addon.Serve(spec()) }

// sampleMessages is the sample chat as the Settings previews draw it.
func sampleMessages() (msgs []wireMessage, pin wireMessage) {
	at := time.Date(2026, 11, 21, 19, 41, 0, 0, time.UTC)
	for i, n := range []int{0, 2, 6, 12} {
		msgs = append(msgs, wire(fakeMessage(n, at.Add(time.Duration(i+1)*time.Minute))))
	}
	return msgs, wire(fakeMessage(5, at))
}

// sampleSources is the status chips of the previews.
func sampleSources() []wireSource {
	return []wireSource{
		{Platform: YouTube, Label: "@yourchannel", State: stLive, Viewers: 21, ID: "youtube-1", CanReply: true, ReplyAs: "yourchannel"},
		{Platform: Facebook, Label: "123456789012345", State: stLive, Viewers: 13, ID: "facebook-2", CanReply: true, ReplyAs: "Your Page"},
		{Platform: Twitch, Label: "yourchurch", State: stLive, Note: twitchNote, Viewers: -1, ID: "twitch-3", CanReply: true, ReplyAs: "yourchurch"},
	}
}

// commentsSample is the Live chat (comments only) widget's preview: no
// composer, no send result.
func commentsSample() map[string]any {
	msgs, _ := sampleMessages()
	return map[string]any{
		"connected": true, "sample": false, "source": "YOUTUBE + FACEBOOK + TWITCH", "live": true, "note": "",
		"sources": sampleSources(), "watching": 34, "total": 128, "messages": msgs, "pinned": nil, "hiddenCount": 0,
	}
}

// replySample is the Live chat + reply widget's preview.
func replySample() map[string]any {
	msgs, pin := sampleMessages()
	pin.OnScreen = true
	at := time.Date(2026, 11, 21, 19, 46, 0, 0, time.UTC)
	msgs = append(msgs, wireMessage{ID: "you:s1", Platform: "you", Author: "yourchannel", At: at.Format(time.RFC3339),
		Text: "Welcome! Today's verse: John 3:16", Mine: true,
		Sent: []sentMark{{Platform: YouTube, OK: true}, {Platform: Twitch, OK: true}, {Platform: Facebook, OK: false}}})
	return map[string]any{
		"connected": true, "sample": false, "source": "YOUTUBE + FACEBOOK + TWITCH", "live": true, "note": "",
		"sources": sampleSources(), "canSend": true, "lastSend": nil,
		"watching": 34, "total": 128, "messages": msgs, "pinned": pin, "hiddenCount": 0,
	}
}

func link(label, url string) *addon.GuideLink { return &addon.GuideLink{Label: label, URL: url} }

// replyField is a platform's "Reply from jadeck (optional)" action field:
// a button on the saved connection that runs reply_signin.
func replyField(button, help string) addon.Field {
	return addon.Field{Key: "replySignIn", Label: "Reply from jadeck (optional)", Type: addon.FieldAction, Action: "reply_signin",
		Placeholder: button, Help: help}
}

const replyHelp = "Reading needs no sign-in. To reply, sign in once as the account to reply as."

// widgetStep is every platform guide's step after the reading setup.
var widgetStep = addon.GuideStep{Text: "Add the Live chat widget (comments only) or Live chat + reply to a dashboard."}

// platformType is a platform connection type: no widgets, its Test line
// is that platform's status.
func platformType(id, label, desc string, fields []addon.Field, needs string, g *addon.Guide, sample map[string]any, sampleTest string) addon.ConnectionType {
	return addon.ConnectionType{ID: id, Label: label, Capabilities: []string{addon.CapTool}, Description: desc,
		Fields: fields, Needs: needs, Guide: g, Sample: sample, SampleTest: sampleTest}
}

func connectionTypes() []addon.ConnectionType {
	return []addon.ConnectionType{
		{
			// The hub every widget binds to: the controller makes it by
			// itself (Auto), so nobody adds a "Live chat" connection.
			ID: "chat", Label: "Live chat", Capabilities: []string{addon.CapTool}, Auto: true,
			Description: "Merges every platform chat connection (YouTube, Facebook, Twitch, Rumble, Owncast) for the Live chat widgets and their actions. " +
				"With no platform connection, the widgets show a sample chat.",
			SampleTest: "YouTube (@yourchannel): live (21 watching) · Twitch (yourchurch): " + twitchNote,
		},
		platformType("youtube", "YouTube chat", "Reads a YouTube channel's live chat with an API key (no sign-in). It finds the live stream by itself. Replying is optional: sign in with Google.",
			[]addon.Field{
				{Key: "youtubeKey", Label: "YouTube API key", Type: "password", Help: "From Google Cloud → Credentials."},
				{Key: "youtubeChannel", Label: "YouTube channel or live video", Type: "text", Placeholder: "@yourchannel or UCxxxxxxxxxxxxxxxxxxxxxx",
					Help: "@handle, UC… id, or a live video's link."},
				{Key: "youtubePollSeconds", Label: "YouTube chat check (seconds)", Type: "number", Default: 8, Min: addon.Num(3), Max: addon.Num(60), Advanced: true,
					Help: "Each check costs 5 quota units: 8 s is about 2,250 an hour."},
				{Key: "youtubeClientId", Label: "OAuth client ID", Type: "text", Placeholder: "…apps.googleusercontent.com",
					Help: "Only to reply: a \"TVs and Limited Input devices\" client (setup steps 6-7)."},
				{Key: "youtubeClientSecret", Label: "OAuth client secret", Type: "password", Help: "Only to reply: that client's secret."},
				replyField("Sign in with Google to reply", replyHelp),
			},
			"API key · 5 min",
			&addon.Guide{Minutes: 5,
				Steps: []addon.GuideStep{
					{Text: "Open Google Cloud and create a project (or pick one you have).", Link: link("Create a project", "https://console.cloud.google.com/projectcreate")},
					{Text: "Turn on the YouTube Data API v3 for that project: press Enable.", Link: link("YouTube Data API v3", "https://console.cloud.google.com/apis/library/youtube.googleapis.com")},
					{Text: "Go to Credentials → Create credentials → API key. Copy the key and paste it in \"YouTube API key\".", Link: link("Credentials", "https://console.cloud.google.com/apis/credentials")},
					{Text: "Optional: edit the key and restrict it to the YouTube Data API v3."},
					{Text: "Enter the channel's @handle, its UC… id, or a link to the live video."},
					widgetStep,
					{Text: "Reply from jadeck (optional): in Google Cloud → APIs & Services → OAuth consent screen, choose External, and add the channel's Google account as a test user (or publish the app).", Link: link("OAuth consent screen", "https://console.cloud.google.com/apis/credentials/consent")},
					{Text: "Credentials → Create credentials → OAuth client ID, type \"TVs and Limited Input devices\". Paste its client ID and secret in \"OAuth client ID\" and \"OAuth client secret\", and save.", Link: link("Credentials", "https://console.cloud.google.com/apis/credentials")},
					{Text: "Press \"Sign in with Google to reply\", open google.com/device on any phone or computer, type the code, and pick the channel's account.", Code: "google.com/device"},
				},
				Note: "A key gets 10,000 quota units a day; a 2-hour service at the default 8 s uses about 4,500, and each reply costs 20. The stream must be public or unlisted, with chat on. " +
					"While the OAuth app stays in Testing, Google ends the sign-in after 7 days: sign in again, or publish the app.",
			},
			map[string]any{"youtubeKey": "••••••", "youtubeChannel": "@yourchannel"},
			"YouTube (@yourchannel): live (21 watching)"),
		platformType("facebook", "Facebook Page chat", "Reads the comments on a Facebook Page's live video with a Page access token. It finds the live video by itself. Replies post as the Page (optional).",
			[]addon.Field{
				{Key: "facebookPage", Label: "Facebook Page ID", Type: "text", Placeholder: "123456789012345", Help: "The Page's number (the id from me/accounts)."},
				{Key: "facebookToken", Label: "Facebook Page access token", Type: "password", Help: "The Page's access_token from me/accounts."},
				replyField("Check the Page token", "To reply as the Page, the token needs pages_manage_engagement (step 2)."),
			},
			"Page token · 10 min",
			&addon.Guide{Minutes: 10,
				Steps: []addon.GuideStep{
					{Text: "Create a Meta developer app: choose \"Other\", then \"Business\". It can stay in development mode.", Link: link("Create an app", "https://developers.facebook.com/apps/creation/")},
					{Text: "Open Graph API Explorer, pick your app,press Get Page Access Token, and allow pages_read_engagement, pages_read_user_content and pages_show_list. To reply from jadeck too (optional), also allow pages_manage_engagement.", Link: link("Graph API Explorer", "https://developers.facebook.com/tools/explorer/")},
					{Text: "Copy that token into the Access Token Debugger and press Extend Access Token. Copy the longer token it gives.", Link: link("Access Token Debugger", "https://developers.facebook.com/tools/debug/accesstoken/")},
					{Text: "Back in the Explorer, paste the extended token and run this:", Code: "me/accounts?fields=id,name,access_token"},
					{Text: "Find your Page in the answer. Paste its access_token in \"Facebook Page access token\" (it doesn't expire) and its id in \"Facebook Page ID\"."},
					widgetStep,
					{Text: "Reply from jadeck (optional): save, then press \"Check the Page token\": it says whether replies can post as the Page. Replies to one comment go under it; a message to everyone is a comment on the live video."},
				},
				Note: "Works with Pages only (not personal profiles). Looks for a live video on the Page once a minute and reads its comments every 5 s.",
			},
			map[string]any{"facebookPage": "123456789012345", "facebookToken": "••••••"},
			"Facebook (123456789012345): live (13 watching)"),
		platformType("twitch", "Twitch chat", "Reads a Twitch channel's public chat as a guest (no key, no sign-in). Replying is optional: sign in with Twitch.",
			[]addon.Field{
				{Key: "channel", Label: "Twitch channel", Type: "text", Placeholder: "yourchurch", Help: "The name from twitch.tv/<name>, or the link."},
				{Key: "twitchClientId", Label: "Twitch app client ID", Type: "text", Help: "Only to reply: your Twitch app's Client ID (How to set up, steps 3-4)."},
				replyField("Sign in to Twitch to reply", replyHelp),
			},
			"Channel name · no key",
			&addon.Guide{Minutes: 1,
				Steps: []addon.GuideStep{
					{Text: "Nothing to create: Twitch chat is public. Type the channel name from its address in \"Twitch channel\".", Code: "twitch.tv/yourchurch → yourchurch", Link: link("Twitch", "https://www.twitch.tv/")},
					{Text: "Press Test."},
					widgetStep,
					{Text: "Reply from jadeck (optional): in the Twitch developer console, Register Your Application: any name, OAuth Redirect URL http://localhost, Category \"Chat Bot\" (or Other), Client Type Public.", Code: "http://localhost", Link: link("Register an app", "https://dev.twitch.tv/console/apps/create")},
					{Text: "Copy its Client ID into \"Twitch app client ID\" and save. (A Public app has no secret.)"},
					{Text: "Press \"Sign in to Twitch to reply\", open twitch.tv/activate on any phone or computer, type the code, and sign in with the account replies should come from.", Code: "twitch.tv/activate"},
				},
				Note: "Viewer counts need a Twitch sign-in (not in this version). A reply sign-in lasts up to 30 days; then sign in again.",
			},
			map[string]any{"channel": "yourchurch"},
			"Twitch (yourchurch): "+twitchNote),
		platformType("rumble", "Rumble chat", "Reads your Rumble live stream's chat and rants through your channel's Live Stream API link.",
			[]addon.Field{
				{Key: "apiUrl", Label: "Live Stream API link", Type: "password", Placeholder: "https://rumble.com/-livestream-api/get-data?key=…",
					Help: "Keep it private: it works like a password."},
			},
			"API link · 2 min",
			&addon.Guide{Minutes: 2,
				Steps: []addon.GuideStep{
					{Text: "Sign in to Rumble and open Account → Live Stream API.", Link: link("Live Stream API", "https://rumble.com/account/livestream-api")},
					{Text: "Copy the link it shows. Keep it private: it works like a password."},
					{Text: "Paste it in \"Live Stream API link\" and press Test."},
					widgetStep,
				},
				Note: "Rumble shares the last 50 messages and rants while you're live. Its API is read only: jadeck can't reply on Rumble.",
			},
			map[string]any{"apiUrl": "••••••"},
			"Rumble (yourchannel): live (8 watching)"),
		platformType("owncast", "Owncast chat", "Reads your Owncast server's chat as an anonymous chat user (\"jadeck\" unless you change \"Reply as\"); replies post under that name.",
			[]addon.Field{
				{Key: "server", Label: "Owncast server address", Type: "text", Placeholder: "https://live.example.org",
					Help: "As you open it in a browser (http://192.168.1.50:8080 works too)."},
				{Key: "replyAs", Label: "Reply as", Type: "text", Default: "jadeck", Placeholder: "jadeck", Help: "The chat name replies show under (no sign-in)."},
				replyField("Check", "No sign-in: replies post under the \"Reply as\" name."),
			},
			"Server address",
			&addon.Guide{Minutes: 1,
				Steps: []addon.GuideStep{
					{Text: "Type your Owncast server's address as you open it in a browser.", Code: "https://live.example.org"},
					{Text: "Press Test."},
					widgetStep,
					{Text: "Reply from jadeck (optional): type the name replies should show under in \"Reply as\" (\"jadeck\" by default). Nothing to sign in to."},
				},
				Note: "Reads chat as an anonymous chat user under the \"Reply as\" name. A server set to let only signed-in users chat refuses replies (the result says so).",
			},
			map[string]any{"server": "https://live.example.org", "replyAs": "jadeck"},
			"Owncast (live.example.org): live (5 watching)"),
	}
}

// platformsField is both widgets' platform pick (empty: all).
var platformsField = addon.Field{Key: "platforms", Label: "Platforms", Type: addon.FieldSelect, Multiple: true,
	Options: []addon.Option{{Value: YouTube, Label: "YouTube"}, {Value: Facebook, Label: "Facebook"}, {Value: Twitch, Label: "Twitch"},
		{Value: Rumble, Label: "Rumble"}, {Value: Owncast, Label: "Owncast"}},
	Help: "Empty: every platform."}

func spec() addon.Spec {
	return addon.Spec{
		Manifest: addon.Manifest{
			ID: "chat", Name: "Live chat", Version: Version, Publisher: "jadeck", APIVersion: addon.APIVersion, Repo: "toolingdev/jadeck-addon-chat",
			Description: "YouTube, Facebook, Twitch, Rumble and Owncast chat in one list: just the comments, or reply too.",
			Reaches: "YouTube Data API and Facebook Graph API (with the key and Page token you enter), Twitch chat, Rumble's Live Stream API, " +
				"and your Owncast server. Show on screen goes through your vMix connection (the vMix add-on). To reply: Google and Twitch sign-in " +
				"(oauth2.googleapis.com, id.twitch.tv) and the Twitch Helix API; sign-ins are kept in a file only you can read on this PC",
			ConnectionTypes: connectionTypes(),
			Actions: []addon.ActionDef{
				{ID: "mark_on_screen", Label: "Mark chat message on screen",
					Description: "Mark a message ON SCREEN (and pinned) on every screen. The Live chat + reply widget sends it after filling the vMix title through your vMix connection; it doesn't touch vMix itself.",
					Params:      []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}}},
				{ID: "clear_on_screen", Label: "Clear the on-screen mark", Description: "Take the ON SCREEN mark (and its pin) off on every screen. It doesn't touch vMix."},
				{ID: "pin", Label: "Pin chat message", Description: "Keep a message at the top of the Live chat widget (every screen sees it).",
					Params: []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}}},
				{ID: "unpin", Label: "Unpin chat message", Description: "Take the pin off (a message on screen stays pinned until cleared)."},
				{ID: "hide", Label: "Hide chat message here", Description: "Hide a message from the widget on every screen (not from the platform).",
					Params: []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}}},
				{ID: "show_hidden", Label: "Show hidden chat messages"},
				{ID: "comment_addressed", Label: "Mark a comment addressed",
					Description: "Tick a comment off as dealt with (the speaker read it out or answered it): it fades on every screen and leaves the NEW count. addressed: false brings it back.",
					Params:      []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}, {Key: "addressed", Label: "Addressed", Type: "bool"}}},
				{ID: "send_message", Label: "Send a chat message", MacroStep: true,
					Description: "Post a message in the live chats as the signed-in accounts: to everyone (every platform that can reply, or the ones named), or a reply to one message (only its platform). Answers what got through, per platform.",
					Params: []addon.Field{
						{Key: "text", Label: "Message", Type: "text", Required: true, Help: "At most 200 characters for YouTube, 500 for the others."},
						{Key: "platforms", Label: "Platforms", Type: "text", Help: "Comma list (youtube, facebook, twitch, owncast). Empty: every one that can reply."},
						{Key: "replyTo", Label: "Reply to (message id)", Type: "text", Help: "Empty: a message to everyone."},
					}},
				{ID: "retry_send", Label: "Retry a chat message", Description: "Send a message again to only the platforms it failed on.",
					Params: []addon.Field{{Key: "sendId", Label: "Send id", Type: "text", Help: "Empty: the last one."}}},
				{ID: "dismiss_send", Label: "Dismiss the send result", Description: "Close the \"Sent to N of M\" card on every screen."},
				{ID: "reply_signin", Label: "Sign in to reply",
					Description: "The \"Reply from jadeck\" field on a YouTube, Twitch, Facebook or Owncast chat connection: sign in (a code to type on any device), sign out, or check."},
			},
			Widgets: []addon.WidgetDef{
				{Kind: addon.KindFrame, Type: "live_chat", Label: "Live chat",
					Description: "Just the comments from every platform, big and easy to read. No reply box.",
					Entry:       "ui/comments.html", Size: &addon.Size{W: 5, H: 10}, MinPx: &addon.Size{W: 220, H: 180}, Sample: commentsSample(),
					Fields: []addon.Field{platformsField,
						{Key: "tap", Label: "Let people tap comments", Type: addon.FieldBool, Default: false,
							Help: "On: tapping a comment offers Pin and Hide here. Off: tapping does nothing."},
						{Key: "ack", Label: "Tick comments off (✓)", Type: addon.FieldBool, Default: false,
							Help: "For the speaker: new comments are outlined with a ✓ button; ticked ones fade (on every screen), the header counts what's left, and New only hides the rest."}}},
				{Kind: addon.KindFrame, Type: "live_chat_reply", Label: "Live chat + reply",
					Description: "The comments plus a reply box, Show on screen, Pin and Hide.",
					Entry:       "ui/chat.html", Size: &addon.Size{W: 5, H: 10}, MinPx: &addon.Size{W: 220, H: 180}, Sample: replySample(),
					Fields: []addon.Field{platformsField,
						{Key: "vmix", Label: "vMix connection", Type: addon.FieldConnection, Addon: "vmix", Help: "For Show on screen. Blank: off."},
						{Key: "vmixTitle", Label: "Title for comments", Type: addon.FieldSelect, OptionsFrom: "vmix.titles", OptionsConn: "vmix",
							Help: "A GT title in vMix Social's layout: Message, FromName, Title, FromPhoto."},
						{Key: "vmixHeading", Label: "Title heading", Type: addon.FieldText, Default: "Live Comments", Advanced: true,
							Help: "Goes in the title's Title.Text field."}}},
			},
			Changelog: "0.2.2: Swipes on a phone no longer get dropped when a new comment comes in. 0.2.1: Comment lists scroll the phone's own way: flick and they glide. 0.2.0: Tick comments off (✓) in the Live chat widget: new ones outlined, ticked ones fade on every screen, a NEW count and New only. 0.1.0: first release: YouTube, Facebook, Twitch, Rumble and Owncast chat, each its own connection with a setup guide, merged in two widgets: Live chat (just the comments, big) and Live chat + reply (replies to one comment or every platform at once, Show on screen through your vMix connection, pin and hide here).",
		},
		Open: map[string]addon.OpenFunc{
			"chat":     func(raw json.RawMessage) (any, error) { return Open(raw) },
			"youtube":  openYouTube,
			"facebook": openFacebook,
			"twitch":   openTwitch,
			"rumble":   openRumble,
			"owncast":  openOwncast,
		},
	}
}
