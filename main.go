// Live chat: a jadeck add-on. YouTube, Facebook, Twitch, Rumble and
// Owncast chat, each its own connection, merged in one widget; reply to
// one comment or message every platform at once; and any message put on
// screen through a vMix title.
package main

import (
	"encoding/json"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// Version is set by the release build.
var Version = "0.1.0"

func main() { addon.Serve(spec()) }

// sampleState is the widget as the Settings preview draws it.
func sampleState() map[string]any {
	at := time.Date(2026, 11, 21, 19, 41, 0, 0, time.UTC)
	var msgs []wireMessage
	for i, n := range []int{1, 2, 3, 6} {
		msgs = append(msgs, wire(fakeMessage(n, at.Add(time.Duration(i+1)*time.Minute))))
	}
	pin := wire(fakeMessage(5, at))
	pin.OnScreen = true
	msgs = append(msgs, wireMessage{ID: "you:s1", Platform: "you", Author: "yourchannel", At: at.Add(5 * time.Minute).Format(time.RFC3339),
		Text: "Welcome! Today's verse: John 3:16", Mine: true,
		Sent: []sentMark{{Platform: YouTube, OK: true}, {Platform: Twitch, OK: true}, {Platform: Facebook, OK: false}}})
	return map[string]any{
		"connected": true, "sample": false, "source": "YOUTUBE + FACEBOOK + TWITCH", "live": true, "note": "",
		"sources": []wireSource{
			{Platform: YouTube, Label: "@yourchannel", State: stLive, Viewers: 21, ID: "youtube-1", CanReply: true, ReplyAs: "yourchannel"},
			{Platform: Facebook, Label: "123456789012345", State: stLive, Viewers: 13, ID: "facebook-2", CanReply: true, ReplyAs: "Your Page"},
			{Platform: Twitch, Label: "yourchurch", State: stLive, Note: twitchNote, Viewers: -1, ID: "twitch-3", CanReply: true, ReplyAs: "yourchurch"},
		},
		"canSend": true, "lastSend": nil,
		"watching": 34, "total": 128, "messages": msgs, "pinned": pin, "hiddenCount": 0,
		"canShow": true, "screenLabel": "on screen in vMix (Social 1)",
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

// platformType is a platform connection type: no widgets, its Test line
// is that platform's status.
func platformType(id, label, desc string, fields []addon.Field, needs string, g *addon.Guide, sample map[string]any, sampleTest string) addon.ConnectionType {
	return addon.ConnectionType{ID: id, Label: label, Capabilities: []string{addon.CapTool}, Description: desc,
		Fields: fields, Needs: needs, Guide: g, Sample: sample, SampleTest: sampleTest}
}

func connectionTypes() []addon.ConnectionType {
	return []addon.ConnectionType{
		{
			ID: "chat", Label: "Live chat", Capabilities: []string{addon.CapTool},
			Description: "The Live chat widget and its actions: merges every platform chat connection (YouTube, Facebook, Twitch, Rumble, Owncast) " +
				"and fills a vMix title for Show on screen. With no platform connection, the widget shows a sample chat.",
			Needs: "Add this first",
			Guide: &addon.Guide{
				Steps: []addon.GuideStep{
					{Text: "Add a platform connection below for each place you stream: YouTube, Facebook, Twitch, Rumble or Owncast chat."},
					{Text: "For Show on screen: in vMix, open Settings → Web Controller and tick Enabled, then type that PC's address in \"vMix address\"."},
					{Text: "Type the name of the vMix GT title input that shows comments in \"vMix title input\". It needs vMix Social's layout: Message, FromName, Title, FromPhoto."},
					{Text: "Add the Live chat widget to a dashboard."},
				},
				Note: "With no platform connection, the widget shows a sample chat so you can place and size it first.",
			},
			Fields: []addon.Field{
				{Key: "vmixHost", Label: "vMix address", Type: "text", Default: "127.0.0.1:8088", Placeholder: "127.0.0.1:8088",
					Help: "127.0.0.1:8088 when vMix runs on this PC."},
				{Key: "vmixTitle", Label: "vMix title input", Type: "text", Placeholder: "Social 1",
					Help: "Name or number of the comments title. Empty: Show on screen is off."},
				{Key: "vmixTitleText", Label: "Title heading", Type: "text", Default: "Live Comments", Advanced: true,
					Help: "Goes in the title's Title.Text field."},
				{Key: "vmixUser", Label: "vMix username", Type: "text", Advanced: true, Help: "Only if the Web Controller has a password."},
				{Key: "vmixPassword", Label: "vMix password", Type: "password", Advanced: true},
			},
			Sample:     map[string]any{"vmixHost": "127.0.0.1:8088", "vmixTitle": "Social 1"},
			SampleTest: "YouTube (@yourchannel): live (21 watching) · Twitch (yourchurch): " + twitchNote + " · vMix: answered",
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
					{Text: "Reply from jadeck (optional): type the name replies should show under in \"Reply as\" (\"jadeck\" by default). Nothing to sign in to."},
				},
				Note: "Reads chat as an anonymous chat user under the \"Reply as\" name. A server set to let only signed-in users chat refuses replies (the result says so).",
			},
			map[string]any{"server": "https://live.example.org", "replyAs": "jadeck"},
			"Owncast (live.example.org): live (5 watching)"),
	}
}

func spec() addon.Spec {
	return addon.Spec{
		Manifest: addon.Manifest{
			ID: "chat", Name: "Live chat", Version: Version, Publisher: "jadeck", APIVersion: addon.APIVersion, Repo: "toolingdev/jadeck-addon-chat",
			Description: "YouTube, Facebook, Twitch, Rumble and Owncast chat in one widget: reply, and show messages in vMix.",
			Reaches: "YouTube Data API and Facebook Graph API (with the key and Page token you enter), Twitch chat, Rumble's Live Stream API, " +
				"your Owncast server, and the vMix PC you name (Web Controller, port 8088). To reply: Google and Twitch sign-in " +
				"(oauth2.googleapis.com, id.twitch.tv) and the Twitch Helix API; sign-ins are kept in a file only you can read on this PC",
			ConnectionTypes: connectionTypes(),
			Actions: []addon.ActionDef{
				{ID: "show_on_screen", Label: "Show chat message on screen", MacroStep: true,
					Description: "Put a chat message in the vMix title (SetText: Title, FromName, Message; SetImage: FromPhoto). No id: the newest message. Bringing the title on air stays a vMix overlay.",
					Params:      []addon.Field{{Key: "id", Label: "Message id", Type: "text", Help: "Empty: the newest message."}}},
				{ID: "clear_screen", Label: "Clear chat message from screen", MacroStep: true, Description: "Empty the vMix title's name, message and photo."},
				{ID: "pin", Label: "Pin chat message", Description: "Keep a message at the top of the Live chat widget (every screen sees it).",
					Params: []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}}},
				{ID: "unpin", Label: "Unpin chat message", Description: "Take the pin off (a message on screen stays pinned until cleared)."},
				{ID: "hide", Label: "Hide chat message here", Description: "Hide a message from the widget on every screen (not from the platform).",
					Params: []addon.Field{{Key: "id", Label: "Message id", Type: "text", Required: true}}},
				{ID: "show_hidden", Label: "Show hidden chat messages"},
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
					Description: "Every platform's chat in one list, newest at the bottom, with a status chip per platform; Super Chats, bits, subs and rants in green. Tap a message to reply, show it on screen in vMix, pin it, or hide it here; message every platform at once from the box at the bottom.",
					Entry:       "ui/chat.html", Size: &addon.Size{W: 5, H: 10}, MinPx: &addon.Size{W: 220, H: 180}, Sample: sampleState()},
			},
			Changelog: "First release: YouTube, Facebook, Twitch, Rumble and Owncast chat, each its own connection, merged in one Live chat widget, with replies (to one comment, or to every platform at once), Show on screen through a vMix title, pin and hide here, and a setup guide per platform.",
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
