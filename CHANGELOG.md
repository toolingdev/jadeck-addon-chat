# Changelog

## 0.2.0

- **Tick comments off (✓):** a Live chat widget setting for the speaker.
  New comments are outlined with a ✓ button; ticked ones fade and say
  ADDRESSED on every screen; the header counts what's left, and New only
  hides the rest. Tap ✓ again to bring one back. Action:
  `comment_addressed` (the one action a remote viewer limited to their
  own screen may run).

## 0.1.0

Needs jadeck 0.6 or newer (frame widgets and the shared frame kit) and add-on
SDK v0.5.0 (setup guides, action fields, automatic connection types,
connection fields in widget settings and actions run through them).

- **One connection per platform:** YouTube chat, Facebook Page chat,
  Twitch chat, Rumble chat and Owncast chat, each added, changed or
  removed on its own without restarting the others. There's no "Live
  chat" connection to add: the controller makes the one the widgets use
  by itself (needs SDK v0.5.0's automatic connection types).
- **Setup guides:** every connection type says what it needs and walks
  through getting it, step by step, with links to the right pages.
- **Two widgets, independent of the connections:**
  - **Live chat**: just the comments from every platform, big and easy
    to read (platform and name over the words), no reply box and no
    buttons. Setting **Let people tap comments** (off by default) lets a
    tap Pin or Hide here.
  - **Live chat + reply**: everything below: one status chip per
    platform, the reply box, Show on screen, Pin and Hide here.
  - Both: newest at the bottom (it follows new messages unless you've
    scrolled up); Super Chats, Super Stickers, Twitch bits and subs, and
    Rumble rants in green with the amount; a **Platforms** setting
    (empty: all) per widget, filtering messages, chips and the watching
    count; they fit from 220 × 180 px up.
- **Twitch:** anonymous chat, no key; deleted and timed-out messages go.
- **Rumble:** chat and rants through your Live Stream API link.
- **Owncast:** chat from your own server, read as an anonymous viewer.
- **YouTube** finds the channel's live stream by itself within the API
  quota; **Facebook** finds the Page's live video by itself.
- **Show on screen** (Live chat + reply): through the vMix connection
  picked in the widget's settings (the vMix add-on's `title_text` /
  `title_image`), into a GT title in vMix Social's layout (Message,
  FromName, Title, FromPhoto) picked from that vMix's titles; then the
  message shows ON SCREEN, pinned in amber, on every screen. Clear from
  screen empties it. Actions `mark_on_screen` and `clear_on_screen`.
- **Pin** and **Hide here**, shared by every screen; removing a platform
  connection keeps its messages and the pin.
- With no platform connection, a sample chat to place and size the widget.
- **Replies:** tap a message → **↩ Reply** (its platform only), or
  message every platform at once from the box at the bottom (**SEND TO**
  chips, tap to leave one out; "Send to N"). Your message shows at once
  as **YOU · <account>** with ✓ / ✕ per platform, and the platforms'
  copies of it aren't shown twice. A result card says "Sent to 2 of 3",
  why each failure happened, **Retry** only the failed ones, **Dismiss**.
  In a small widget the box opens over the chat from the header.
  (Live chat + reply.)
  - **YouTube:** sign in with Google (a code typed on any device);
    `liveChatMessages.insert`; replies as "@author text".
  - **Facebook:** as the Page, with `pages_manage_engagement` on the Page
    token; replies go under the comment.
  - **Twitch:** sign in with Twitch (a code typed on any device, a Public
    app's client ID); real Twitch replies.
  - **Owncast:** under the "Reply as" name, no sign-in.
  - **Rumble:** read only.
  - Sign-ins survive restarts in a file only this user can read; Sign out
    deletes them. Actions `send_message`, `retry_send`, `dismiss_send`
    and each connection's `reply_signin` field.
