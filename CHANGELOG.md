# Changelog

## 0.1.0 (unreleased)

Needs jadeck 0.6 or newer (frame widgets and the shared frame kit; add-on
SDK v0.3.0, with setup guides).

- **One connection per platform:** YouTube chat, Facebook Page chat,
  Twitch chat, Rumble chat and Owncast chat, each added, changed or
  removed on its own without restarting the others. The **Live chat**
  connection merges every one that's open and holds the vMix settings.
- **Setup guides:** every connection type says what it needs and walks
  through getting it, step by step, with links to the right pages.
- **Live chat widget:** every platform in one list, newest at the bottom
  (it follows new messages unless you've scrolled up); Super Chats,
  Super Stickers, Twitch bits and subs, and Rumble rants in green with the
  amount; author names in the platform's colour; one status chip per
  platform (live with viewers, waiting, or a problem); message count in
  the header.
- **Twitch:** anonymous chat, no key; deleted and timed-out messages go.
- **Rumble:** chat and rants through your Live Stream API link.
- **Owncast:** chat from your own server, read as an anonymous viewer.
- **YouTube** finds the channel's live stream by itself within the API
  quota; **Facebook** finds the Page's live video by itself.
- **Show on screen:** fills a vMix GT title in vMix Social's layout
  (Title, FromName, Message, FromPhoto); the message stays pinned in
  amber while it's up. Take off screen empties it. Also macro steps.
- **Pin** and **Hide here**, shared by every screen; removing a platform
  connection keeps its messages and the pin.
- With no platform connection, a sample chat to place and size the widget.
