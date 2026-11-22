# Live chat — jadeck add-on

Your live stream's chat from **YouTube**, **Facebook**, **Twitch**,
**Rumble** and **Owncast** in one dashboard widget, newest at the bottom,
Super Chats, bits, subs and rants in green, and one status chip per
platform under the header. Tap a message to:

- **Show on screen**: put it in a vMix title (vMix Social's layout), so
  the booth can bring it on air as an overlay;
- **Pin**: keep it at the top of the widget on every screen;
- **Hide here**: hide it from the widget (not from the platform).

This version is read-only. Replying needs each platform's sign-in and
comes later.

## How it fits together

Each platform is its **own connection** (Settings → Connections → Add),
so you can add, change or remove one without touching the others:

| Connection | What it needs |
|---|---|
| **Live chat** | Add this first · vMix optional |
| **YouTube chat** | API key · about 5 min |
| **Facebook Page chat** | Page token · about 10 min · Page admin |
| **Twitch chat** | Channel name · no key |
| **Rumble chat** | Live Stream API link · about 2 min |
| **Owncast chat** | Server address |

The **Live chat** connection is the one the widget, macros and agent
tools use. It merges every platform connection of this add-on that's
open. Removing a platform connection mid-service keeps its messages and
the pin; the other platforms keep running. Two connections of one
platform (say, two YouTube channels) both show, each with its own chip.

Every connection has a **Test** button: it says in one line what that
platform is doing ("Twitch (yourchurch): chat connected (Twitch doesn't
share viewer counts without sign-in)").

## Live chat (the widget and vMix)

1. Add a platform connection for each place you stream (below).
2. For Show on screen: in vMix, Settings → Web Controller → **Enabled**,
   and type that PC's address in "vMix address" (127.0.0.1:8088 when vMix
   runs on the same PC).
3. Type the name of the vMix GT title input that shows comments in "vMix
   title input". It needs vMix Social's layout: Message, FromName, Title,
   FromPhoto.
4. Add the **Live chat** widget to a dashboard.

With no platform connection, the widget shows a **sample chat**, so you
can place and size it first.

Show on screen calls vMix's Web Controller (port 8088):

| vMix function | Field | Value |
|---|---|---|
| SetText | `Title.Text` | "Live Comments" (setting "Title heading") |
| SetText | `FromName.Text` | who wrote it (with the amount for a Super Chat, bits or rant) |
| SetText | `Message.Text` | the message |
| SetImage | `FromPhoto.Source` | their picture, when the platform gives one |

Bring the title on air with a vMix overlay (a vMix widget or macro in
jadeck, or in vMix). "Take off screen" (tap the pinned message) empties
the name, message and photo. Both are also macro steps and agent tools
(`chat_show_on_screen`, `chat_clear_screen`).

## YouTube chat: API key (about 5 min)

1. [Create a Google Cloud project](https://console.cloud.google.com/projectcreate)
   (or pick one you have).
2. Turn on the [YouTube Data API v3](https://console.cloud.google.com/apis/library/youtube.googleapis.com)
   for it.
3. [Credentials](https://console.cloud.google.com/apis/credentials) →
   Create credentials → **API key**; paste it in "YouTube API key".
4. Optional: restrict the key to the YouTube Data API v3.
5. Enter the channel's `@handle`, its `UC…` id, or a live video's link.

Good to know: a key gets 10,000 quota units a day. A 2-hour service at
the default 8 s check uses about 4,500 (each chat check costs 5; looking
for the live stream, 2 a minute while nothing is live; the viewer count,
1 a minute). For a longer day, raise "YouTube chat check (seconds)"
(advanced) or paste the live video's link. When the quota runs out, the
chip says so and the add-on waits 15 minutes. The stream must be public
or unlisted, with chat on.

## Facebook Page chat: Page token (about 10 min, Page admin)

1. [Create a Meta developer app](https://developers.facebook.com/apps/creation/):
   "Other" → "Business". It can stay in development mode.
2. Open [Graph API Explorer](https://developers.facebook.com/tools/explorer/),
   pick the app, press **Get Page Access Token**, and allow
   `pages_read_engagement`, `pages_read_user_content` and `pages_show_list`.
3. Paste that token in the [Access Token Debugger](https://developers.facebook.com/tools/debug/accesstoken/)
   → **Extend Access Token**.
4. Back in the Explorer, with the extended token, run
   `me/accounts?fields=id,name,access_token`. Paste your Page's
   `access_token` (it doesn't expire) in "Facebook Page access token" and
   its `id` in "Facebook Page ID".

Good to know: apps like this can't read personal profiles. Stream to the
Page (or a Restream connection, coming later). The add-on looks for a
live video on the Page once a minute and reads its comments every 5 s.

## Twitch chat: channel name (no key)

1. Nothing to create: Twitch chat is public. Type the channel name from
   its address (`twitch.tv/yourchurch` → `yourchurch`), or paste the link.
2. Press **Test**.

The add-on joins the chat as an anonymous guest (Twitch's read-only
`justinfan` login) over `wss://irc-ws.chat.twitch.tv`. Cheers show their
bits, subs and resubs show as "Sub", and messages a moderator deletes or
times out disappear here too. Good to know: viewer counts and live /
offline need a Twitch sign-in (not in this version), so the chip says
"chat connected".

## Rumble chat: Live Stream API link (about 2 min)

1. Sign in to Rumble and open [Account → Live Stream API](https://rumble.com/account/livestream-api).
2. Copy the link it shows. Keep it private: it works like a password.
3. Paste it in "Live Stream API link" and press **Test**.

Good to know: Rumble shares the last 50 messages and rants while you're
live. The add-on checks every 5 s while a stream is live and every 30 s
while none is. If Rumble refuses the link (it expired or was reset), the
chip says so: copy it again.

## Owncast chat: server address

1. Type your Owncast server's address as you open it in a browser
   (`https://live.example.org`, or `http://192.168.1.50:8080` on your
   network).
2. Press **Test**.

Good to know: the add-on reads chat as an anonymous viewer named
"jadeck"; nothing is posted. It checks the stream's status and viewers
every 30 s. It logs in the way Owncast's own web chat does
(`/api/chat/register`), which isn't a documented, stable Owncast API: if
a server says it "didn't accept an anonymous chat login", its chat may be
off or its version may differ.

## Privacy

The YouTube key, the Facebook token and the Rumble link are saved as
connection passwords (never shown again) and never written to the log or
put in an error. The YouTube key travels in a request header; the
Facebook token in the Graph API request itself, over HTTPS. Twitch and
Owncast are read anonymously. Pin and Hide here live in the add-on
(shared by every screen) and are forgotten when it restarts.

## Build

```bash
go test -race ./...
go run github.com/toolingdev/jadeck/sdk/addon/cmd/ptz-addon build
```

Try it on a controller: Settings → Add-ons → Developer → Load a folder (this
folder), or install `dist/chat.ptzaddon` with Install from file.

Release: push a tag `v1.2.3`; the workflow builds, signs (secret
`PTZ_SIGNING_KEY`) and attaches `chat.ptzaddon` to the GitHub release.
