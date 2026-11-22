# Live chat — jadeck add-on

Your live stream's chat from **YouTube**, **Facebook**, **Twitch**,
**Rumble** and **Owncast** on a dashboard, newest at the bottom, with
Super Chats, bits, subs and rants in green. Two widgets read the same
chat:

- **Live chat**: just the comments from every platform, big and easy
  to read. No reply box and no buttons, for the person up front.
- **Live chat + reply**: the comments plus a reply box, **Show on
  screen** (in vMix), **Pin** and **Hide here**, for the booth or
  whoever answers viewers.

## Quick setup

1. **Add a connection for each platform** you stream to (Settings →
   Connections → Add connection): YouTube chat, Facebook Page chat,
   Twitch chat, Rumble chat or Owncast chat. Each has a "How to set up"
   guide (below).
2. **Add a widget** to a dashboard: **Live chat** or **Live chat +
   reply**. Both read every platform connection you added; there's no
   "Live chat" connection to add (the controller makes the one the
   widgets use by itself).
3. Optional, in the widget's settings: **Platforms** (empty: all) and,
   for Live chat + reply, Show on screen (below).

With no platform connection, the widgets show a **sample chat**, so you
can place and size them first.

| Connection | What it needs |
|---|---|
| **YouTube chat** | API key · about 5 min |
| **Facebook Page chat** | Page token · about 10 min · Page admin |
| **Twitch chat** | Channel name · no key |
| **Rumble chat** | Live Stream API link · about 2 min |
| **Owncast chat** | Server address |

Each platform is its own connection, so you can add, change or remove
one without touching the others. Removing one mid-service keeps its
messages and the pin; the other platforms keep running. Two connections
of one platform (say, two YouTube channels) both show, each with its own
chip. Every connection has a **Test** button: it says in one line what
that platform is doing ("Twitch (yourchurch): chat connected (Twitch
doesn't share viewer counts without sign-in)").

## The widgets

**Live chat** (comments only): each comment is the platform and name in
the platform's colour ("YOUTUBE · Ruth K.") over the words in big text
(about 17 px, smaller in a small box). The header says how many platforms
and how many are watching; a platform with a problem gets a red line
under it. The pinned or on-screen message sits on top in amber. Tapping
does nothing, unless you turn on **Let people tap comments** in its
settings: then a tap offers **Pin** and **Hide here**.

**Live chat + reply**: one status chip per platform under the header,
and tap a message to:

- **Show on screen**: put it in a vMix title (below);
- **Pin**: keep it at the top of the widgets on every screen;
- **Hide here**: hide it from the widgets (not from the platform);
- **↩ Reply**: answer it on its own platform (shown when that platform
  can reply right now).

The box at the bottom sends one message to **every platform** that can
take it ("Send to 3"; tap a platform to leave it out). Reading needs no
sign-in; replying is optional, per platform (see [Replies](#replies)).

**Platforms** (both widgets' settings): pick YouTube, Facebook, Twitch,
Rumble and/or Owncast to show only those: their messages, chips and
watching count (and, for Live chat + reply, the SEND TO chips). Empty
shows every platform. Each widget has its own pick, so a Spanish-language
screen can show only Facebook while the booth sees everything.

## Show on screen (Live chat + reply)

Show on screen uses the **vMix connection** you already have (the vMix
add-on); nothing to type. In the Live chat + reply widget's settings:

1. **vMix connection**: pick it. Blank: Show on screen is off.
2. **Title for comments**: pick the GT title from that vMix's list. It
   needs vMix Social's layout: Message, FromName, Title, FromPhoto.
3. Optional (advanced): **Title heading**, "Live Comments" by default.

Show on screen runs the vMix add-on's own actions on that connection:

| vMix add-on action | Field | Value |
|---|---|---|
| `title_text` | `Message.Text` | the message |
| `title_text` | `FromName.Text` | who wrote it (with the amount for a Super Chat, bits or rant) |
| `title_text` | `Title.Text` | the title heading |
| `title_image` | `FromPhoto.Source` | their picture, when the platform gives one |

With no picture, the photo is emptied (`run_function` SetImage), so the
last person's face doesn't stay. Then the message is marked **ON
SCREEN** (and pinned) on every screen. If vMix refuses, the widget says
why in red and nothing is marked. Bring the title on air with a vMix
overlay (a vMix widget or macro in jadeck, or in vMix). **Clear from
screen** (tap the pinned message) empties the message, name and heading.

The marks are also actions (`mark_on_screen` with a message id,
`clear_on_screen`); they only change the widgets, not vMix.

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

Good to know: this works with Pages only, not personal profiles. The
add-on looks for a live video on the Page once a minute and reads its
comments every 5 s.

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

Good to know: the add-on reads chat as an anonymous chat user named as
"Reply as" says ("jadeck" by default); replies post under that name. It
checks the stream's status and viewers every 30 s. It logs in the way Owncast's own web chat does
(`/api/chat/register`), which isn't a documented, stable Owncast API: if
a server says it "didn't accept an anonymous chat login", its chat may be
off or its version may differ.

## Replies

Two ways to answer, from the Live chat + reply widget (or the
`send_message` action, a macro step and agent tool):

- **Reply to one comment**: tap it → **↩ Reply**. The box says
  "↩ Replying to Ana L. on Facebook" (✕ cancels) and it goes to that
  platform only.
- **Message everyone**: type in the box with no reply picked. The
  **SEND TO** chips are every open platform: amber and ticked will get
  it, tap one to leave it out; grey ones can't take replies (not signed
  in, or Rumble's "read only"). The button says how many: "Send to 3".

The message shows in the chat at once as **YOU · <account>**, with
✓ YouTube ✓ Twitch ✕ Facebook under it. When a platform shows it back,
that copy is dropped (it isn't shown twice). A card says what got
through, "Sent to 2 of 3", with the reason for each failure in plain
words, **Retry <Platform>** (only the ones that failed) and **Dismiss**.

Limits: one send a second per platform; empty messages are refused;
YouTube takes at most 200 characters, the others 500 here (Twitch's own
limit; Facebook's and Owncast's are larger).

In a box shorter than about 260 px or narrower than 220 px (or one too
full for it), the box moves behind a **↩** button in the header and opens
over the chat.

Each platform connection has a **Reply from jadeck (optional)** field.
Sign-ins use a code you type on any phone or computer, so they work from
a tablet too. They're kept across restarts in `jadeck-addon-chat/tokens.json`
in this PC's user config folder (macOS: `~/Library/Application Support`,
Windows: `%AppData%`, Linux: `~/.config`), readable by this user only.
**Sign out** deletes the entry.

### YouTube: sign in with Google

1. In Google Cloud (the project with your API key), open **APIs &
   Services → [OAuth consent screen](https://console.cloud.google.com/apis/credentials/consent)**:
   **External**, and add the channel's Google account as a **test user**
   (or publish the app).
2. **[Credentials](https://console.cloud.google.com/apis/credentials) →
   Create credentials → OAuth client ID**, type **TVs and Limited Input
   devices**. Paste its client ID and secret in "OAuth client ID" and
   "OAuth client secret", and save.
3. Press **Sign in with Google to reply**, open `google.com/device` on
   any phone or computer, type the code, and pick the channel's account.

It asks for the `youtube` permission (Google's device sign-in doesn't
allow `youtube.force-ssl`). Replies go to the live chat being read with
`liveChatMessages.insert` (20 quota units each). YouTube chat has no
threads: a reply is sent as "@Ana L. your text". Good to know: while the
OAuth app stays in **Testing**, Google ends the sign-in after 7 days;
the result says so, and you sign in again (or publish the app).

### Facebook: one more permission on the Page token

No sign-in: replies post as the Page with the token you already pasted,
if it has **pages_manage_engagement**. In the guide's step 2, allow it
too, then get the Page token again. **Check the Page token** says "Can
reply as <Page>" or what's missing. A reply to one comment goes under it
(`POST /{comment-id}/comments`); a message to everyone is a comment on
the live video (`POST /{video-id}/comments`).

### Twitch: sign in with Twitch

1. In the [Twitch developer console](https://dev.twitch.tv/console/apps/create),
   **Register Your Application**: any name, OAuth Redirect URL
   `http://localhost`, Category **Chat Bot** (or Other), Client Type
   **Public**.
2. Copy its **Client ID** into "Twitch app client ID" and save. A Public
   app has no secret.
3. Press **Sign in to Twitch to reply**, open `twitch.tv/activate` on any
   phone or computer, type the code, and sign in with the account replies
   should come from.

It asks for `user:write:chat` and sends with Helix **Send Chat Message**;
a reply to one comment is a real Twitch reply. Sign-ins last up to 30
days (a public app's refresh token works once; each renewal is saved at
once), then sign in again.

### Owncast: a name, no sign-in

Type the name replies show under in **Reply as** ("jadeck" by default).
Replies go over the chat connection the add-on already has; a reply to
one comment is "@Ruth your text". A server set to let only signed-in
users chat (or only established ones) refuses them, and the result says
so.

### Rumble: read only

Rumble's Live Stream API can't post, so its chip is grey ("read only").

## Privacy

The YouTube key, the Facebook token, the Google OAuth client secret and
the Rumble link are saved as connection passwords (never shown again) and
never written to the log or put in an error. Reply sign-ins (Google and
Twitch tokens) are kept in the 0600 tokens file above, never in the
widget's state, a Test line, the log or an error. The YouTube key travels in a request header; the
Facebook token in the Graph API request itself, over HTTPS (in the body
for replies). Twitch and Owncast are read anonymously. Pin and Hide here live in the add-on
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
