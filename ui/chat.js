// Live chat + reply: the widget's platforms (setting "platforms") in one
// list, newest at the bottom (it follows new messages unless you've
// scrolled up), Super Chats / bits / subs / rants in green, the pinned /
// on-screen message in amber on top, and one status chip per platform
// under the header. Tap a message for Reply, Show on screen, Pin and Hide
// here. The composer at the bottom sends to every platform that can reply
// (tap a chip to leave one out) or replies to one message; the result
// card says what got through. In a small box the composer opens over the
// widget from a Reply button in the header.
// Show on screen fills the vMix title picked in this widget's settings
// through the vMix connection picked there (K.actVia "vmix"), then marks
// the message ON SCREEN on every screen (hub action mark_on_screen).
// Header, buttons and status dot are the shared frame kit's (JK); the
// list scrolls by drag / wheel without a scrollbar (common.js).
(function () {
  "use strict";
  var K = window.Kit, JK = window.JK, C = window.Chat, main = document.getElementById("main");
  if (!JK) { K.start(); return; }

  var hdr = JK.header({ title: "Live chat" });
  var chips = K.el("div", "chips");
  var errs = K.el("div", "errs");
  var pin = K.el("div", "pin jk-live"), wrap = K.el("div", "listwrap"), list = K.el("div", "list jk-live"), foot = K.el("div", "foot");
  var footNote = K.el("span", "footnote");
  var jump = JK.button({ label: "↓ Newest", onClick: function () { stick = true; toBottom(); } });
  jump.classList.add("jump");
  var unhide = JK.button({ label: "Show hidden", onClick: function () { return K.act("show_hidden"); } });
  unhide.classList.add("unhide");
  wrap.appendChild(list); wrap.appendChild(jump);
  foot.appendChild(footNote); foot.appendChild(unhide);
  var res = K.el("div", "sendres"), comp = K.el("div", "composer");
  main.appendChild(chips); main.appendChild(errs); main.appendChild(pin); main.appendChild(wrap); main.appendChild(res); main.appendChild(comp); main.appendChild(foot);

  var open = null, pinOpen = false, stick = true, lastSeen = "", settings = {}, scrErr = "", scrErrTimer = 0;
  var plat = C.plat, badge = C.badge, hm = C.hm, SHORT = C.SHORT, superLabel = C.superLabel;
  var scroll = C.scroller(list, onScroll);
  K.busy = scroll.busy;

  // one chip per source: green dot + viewers when live, hollow dot +
  // "waiting", red "!" + a short note on error; the full note in the tooltip
  function drawChips(st, w) {
    chips.textContent = "";
    var list = Array.isArray(st.sources) ? st.sources : [];
    chips.hidden = !list.length;
    list.forEach(function (s) {
      var d = plat(s.platform), c = K.el("span", "chip " + d.cls + " " + (s.state || "waiting"));
      var mark = K.el("i", "mark", s.state === "error" ? "!" : "");
      c.appendChild(mark);
      var name = chipsShort > 1 ? C.SHORT[s.platform] || d.name : d.name;
      if (s.label && s.platform !== "sample" && w >= 300 && !chipsShort) name += " " + s.label;
      c.appendChild(K.el("span", "cname", name));
      var tail = "";
      if (s.state === "live") tail = s.viewers != null && s.viewers >= 0 ? String(s.viewers) : "";
      else if (s.state === "error") tail = "";
      else tail = chipsShort > 1 ? "" : "waiting";
      if (tail) c.appendChild(K.el("span", "ctail", tail));
      c.title = d.name + (s.label ? " (" + s.label + ")" : "") + ": " +
        (s.state === "live" ? "live" + (s.viewers >= 0 ? ", " + s.viewers + " watching" : "") : s.state || "") +
        (s.note ? " — " + s.note : "");
      chips.appendChild(c);
    });
    // what's wrong (the chip only says which), and a Show on screen
    // that vMix refused
    C.errLines(errs, list, [scrErr]);
  }
  function act(name, params) {
    return K.act(name, params).then(function () { open = null; pinOpen = false; K.draw(); }, function () {});
  }

  // ------------------------------------------------------ Show on screen
  // Through the vMix connection and title picked in this widget's
  // settings: the vMix add-on's title_text / title_image actions, then
  // the hub marks it ON SCREEN for every screen.
  function canShow() { return !!(settings.vmix && settings.vmixTitle); }
  function vmixFailed(e) {
    scrErr = "vMix: " + ((e && e.message) || "didn't answer");
    clearTimeout(scrErrTimer);
    scrErrTimer = setTimeout(function () { scrErr = ""; K.draw(); }, 15000);
    K.draw();
  }
  function fillTitle(heading, name, text, photo) {
    var input = settings.vmixTitle;
    function setText(field, value) { return K.actVia("vmix", "title_text", { input: input, field: field, text: value }); }
    return setText("Message.Text", text)
      .then(function () { return setText("FromName.Text", name); })
      .then(function () { return setText("Title.Text", heading); })
      .then(function () {
        // A title without a photo field refuses it; the text is up anyway.
        // No picture (or clearing): empty it, so the last person's face
        // doesn't stay next to someone else's words.
        if (photo) return K.actVia("vmix", "title_image", { input: input, field: "FromPhoto.Source", path: photo }).catch(function () {});
        return K.actVia("vmix", "run_function", { "function": "SetImage", input: input, selectedName: "FromPhoto.Source" }).catch(function () {});
      });
  }
  function showOnScreen(m) {
    if (!canShow()) return null;
    var name = (m.author || "") + (m.super ? " · " + m.super : "");
    return fillTitle(settings.vmixHeading || "Live Comments", name, m.text || "", m.avatar || "")
      .then(function () { scrErr = ""; return act("mark_on_screen", { id: m.id }); }, vmixFailed);
  }
  function clearScreen() {
    if (!canShow()) return act("clear_on_screen");
    return fillTitle("", "", "", "")
      .then(function () { scrErr = ""; return act("clear_on_screen"); }, vmixFailed);
  }
  function bottomGap() { return list.scrollHeight - list.clientHeight - list.scrollTop; }
  function toBottom() { list.scrollTop = list.scrollHeight; jump.hidden = true; jump.set({ label: "↓ Newest" }); }
  function onScroll() {
    stick = bottomGap() <= 4;
    jump.hidden = stick;
    if (stick) jump.set({ label: "↓ Newest" });
  }

  // ------------------------------------------------------------ the rows
  function row(m, st) {
    var r = K.el("div", "msg" + (m.super ? " super" : "") + (m.mine ? " mine" : "") + (m.id === open ? " open" : ""));
    r.dataset.id = m.id;
    r.tabIndex = 0;
    r.setAttribute("aria-expanded", m.id === open ? "true" : "false");
    if (!m.mine) r.appendChild(badge(m.platform));
    var body = K.el("div", "body");
    var meta = K.el("div", "meta");
    if (m.super) meta.appendChild(K.el("span", "amt", superLabel(m) + " · "));
    if (m.mine) {
      meta.appendChild(K.el("span", "who me", "YOU · " + (m.author || "")));
      if (m.replyTo) meta.appendChild(document.createTextNode(" ↩ " + m.replyTo));
    } else meta.appendChild(K.el("span", "who " + plat(m.platform).cls, m.author || ""));
    meta.appendChild(document.createTextNode(" · " + hm(m.at)));
    body.appendChild(meta);
    body.appendChild(K.el("div", "text", m.text || ""));
    if (m.mine && m.sent && m.sent.length) {
      var marks = K.el("div", "marks");
      m.sent.forEach(function (s) {
        marks.appendChild(K.el("span", s.ok ? "ok" : "bad", (s.ok ? "✓ " : "✕ ") + plat(s.platform).name));
      });
      if (m.sent.some(function (s) { return !s.ok; })) marks.appendChild(K.el("span", "hint", "(tap to retry)"));
      body.appendChild(marks);
    }
    if (m.id === open) body.appendChild(actions(m, st));
    r.appendChild(body);
    return r;
  }
  function canReplyOn(st, p) {
    return (st.sources || []).some(function (s) { return s.platform === p && s.canReply; });
  }
  function actions(m, st) {
    var a = K.el("div", "acts");
    if (m.mine) {
      var failed = (m.sent || []).filter(function (s) { return !s.ok; });
      if (failed.length) {
        var retry = JK.button({ label: "Retry " + failed.map(function (s) { return plat(s.platform).name; }).join(" + "), tone: "accent",
          onClick: function () { return act("retry_send", { sendId: m.id.replace(/^you:/, "") }); } });
        retry.classList.add("show");
        a.appendChild(retry);
      }
      a.appendChild(JK.button({ label: "Hide here", onClick: function () { return act("hide", { id: m.id }); } }));
      return a;
    }
    if (canShow()) {
      var show = JK.button({ label: "Show on screen", tone: "accent", title: "Put this message in the vMix title",
        onClick: function () { return showOnScreen(m); } });
      show.classList.add("show");
      a.appendChild(show);
    }
    if (canReplyOn(st, m.platform)) {
      var rb = JK.button({ label: "↩ Reply", tone: "accent", title: "Reply on " + plat(m.platform).name + " only",
        onClick: function () { startReply(m); } });
      rb.classList.add("reply");
      a.appendChild(rb);
    }
    a.appendChild(JK.button({ label: "Pin", onClick: function () { return act("pin", { id: m.id }); } }));
    a.appendChild(JK.button({ label: compact || canReplyOn(st, m.platform) ? "Hide" : "Hide here", onClick: function () { return act("hide", { id: m.id }); } }));
    return a;
  }

  // ------------------------------------------------------------ replying
  // Local to this screen: who's being answered, which chips are unticked,
  // what's typed. The composer is built once (typing survives redraws).
  var replyTo = null, off = {}, overlay = false, ovlOpen = false, compact = false, resCompact = false, lastSt = {}, sendErr = "", hiddenRes = {}, okTimer = null;
  var MIN_LIST = 76, chipsShort = 0; // 1: no labels, 2: short names
  var cHead = K.el("div", "chead"), cTo = K.el("div", "cto"), cRow = K.el("div", "crow"), cHint = K.el("div", "chint");
  var cLabel = K.el("span", "jk-smallcaps", "Send to"), cChips = K.el("div", "tchips");
  var cWho = K.el("span", "cwho");
  var cClose = JK.button({ label: "✕", title: "Cancel the reply", onClick: function () { cancelReply(); } });
  cClose.classList.add("cx");
  var input = document.createElement("input");
  input.type = "text"; input.className = "cin"; input.autocomplete = "off"; input.enterKeyHint = "send";
  input.setAttribute("aria-label", "Message");
  var sendBtn = JK.button({ label: "Send", tone: "accent", onClick: function () { return doSend(); } });
  sendBtn.classList.add("csend");
  cHead.appendChild(cWho); cHead.appendChild(cClose);
  cTo.appendChild(cLabel); cTo.appendChild(cChips);
  cRow.appendChild(input); cRow.appendChild(sendBtn);
  comp.appendChild(cHead); comp.appendChild(cTo); comp.appendChild(cRow); comp.appendChild(cHint);
  var ovlBtn = JK.button({ label: "↩", title: "Reply / message everyone", onClick: function () { ovlOpen = !ovlOpen; K.draw(); if (ovlOpen) focusInput(); } });
  ovlBtn.classList.add("ovlbtn");
  var hdrRight = K.el("span", "hright"), hdrText = K.el("span", "htext");
  hdrRight.appendChild(hdrText); hdrRight.appendChild(ovlBtn);

  function focusInput() { if (!K.touch) setTimeout(function () { try { input.focus(); } catch (e) { /* fine */ } }, 0); }
  function startReply(m) {
    replyTo = { id: m.id, author: m.author, platform: m.platform };
    open = null; sendErr = "";
    if (overlay) ovlOpen = true;
    K.draw();
    focusInput();
  }
  function cancelReply() { replyTo = null; sendErr = ""; if (overlay) ovlOpen = false; K.draw(); }
  input.addEventListener("input", function () { sendErr = ""; updateSend(); });
  input.addEventListener("keydown", function (e) {
    if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); if (!sendBtn.disabled) doSend(); }
    if (e.key === "Escape") { if (replyTo) cancelReply(); else if (ovlOpen) { ovlOpen = false; K.draw(); } }
  });

  // the platforms a message to everyone goes to (ticked and able)
  function sources(st) { return (st.sources || []).filter(function (s) { return s.platform !== "sample"; }); }
  function picked(st) { return sources(st).filter(function (s) { return s.canReply && !off[s.id]; }); }
  function limitFor(st) {
    var ps = replyTo ? [replyTo.platform] : picked(st).map(function (s) { return s.platform; });
    var lim = 500;
    ps.forEach(function (p) {
      var l = p === "youtube" ? 200 : 500;
      if (replyTo && (p === "youtube" || p === "owncast")) l -= (replyTo.author || "").length + 2; // "@author "
      lim = Math.min(lim, l);
    });
    return Math.max(lim, 1);
  }
  function updateSend() {
    var st = lastSt, n = replyTo ? 1 : picked(st).length, text = input.value.trim();
    var replyOK = replyTo && canReplyOn(st, replyTo.platform);
    sendBtn.set({ label: replyTo ? "Send" : "Send to " + n, disabled: K.editing || !text || (replyTo ? !replyOK : n === 0) });
  }
  function doSend() {
    var st = lastSt, text = input.value.trim();
    if (!text) return;
    var p = { text: text };
    if (replyTo) p.replyTo = replyTo.id;
    else p.platforms = picked(st).map(function (s) { return s.id; }).join(",");
    sendErr = "";
    return K.act("send_message", p).then(function () {
      input.value = ""; replyTo = null; if (overlay) ovlOpen = false; K.draw();
    }, function (e) { sendErr = (e && e.message) || "Couldn't send"; K.draw(); });
  }

  function drawComposer(st) {
    var srcs = sources(st), any = srcs.some(function (s) { return s.canReply; });
    if (replyTo && !st.messages.some(function (m) { return m.id === replyTo.id; }) && !(st.pinned && st.pinned.id === replyTo.id)) replyTo = null;
    var show = !st.sample && srcs.length > 0 && (any || replyTo);
    document.body.classList.toggle("ovl", overlay && show);
    document.body.classList.toggle("ovl-open", overlay && show && ovlOpen);
    comp.hidden = !show || (overlay && !ovlOpen);
    ovlBtn.hidden = !(overlay && show);
    ovlBtn.set({ active: ovlOpen });
    if (!show) { ovlOpen = false; return false; }
    cHead.hidden = !replyTo; cTo.hidden = !!replyTo;
    if (replyTo) {
      cWho.textContent = "↩ Replying to " + (replyTo.author || "") + " on " + plat(replyTo.platform).name;
      cWho.title = cWho.textContent;
      cHint.textContent = sendErr || (replyTo.platform === "facebook" ? "Goes only to Facebook, as a reply under " + replyTo.author + "'s comment."
        : replyTo.platform === "twitch" ? "Goes only to Twitch, as a reply to " + replyTo.author + "."
        : "Goes only to " + plat(replyTo.platform).name + ", starting with @" + replyTo.author + ".");
    } else {
      cChips.textContent = "";
      var tight = document.body.classList.contains("narrow");
      srcs.forEach(function (s) {
        if (tight && !s.canReply) return; // no room: only the ones that can take it
        var d = plat(s.platform), on = s.canReply && !off[s.id];
        var name = compact ? (SHORT[s.platform] || d.name) : d.name;
        var label = s.canReply ? (on && !tight ? "✓ " : "") + name : name + (compact ? "" : " · " + (s.replyWhy === "read only" ? "read only" : "can't reply"));
        var b = K.el("button", "tchip " + (s.canReply ? (on ? "on" : "offc") : "dis"), label);
        b.type = "button";
        b.title = d.name + (s.label ? " (" + s.label + ")" : "") + ": " + (s.canReply ? (on ? "will send as " + (s.replyAs || "") + " (tap to leave out)" : "left out (tap to add)") : s.replyWhy || "can't reply");
        b.disabled = !s.canReply;
        b.setAttribute("aria-pressed", on ? "true" : "false");
        b.addEventListener("click", function () { if (K.editing || !s.canReply) return; off[s.id] = !off[s.id]; drawComposer(lastSt); });
        cChips.appendChild(b);
      });
      cHint.textContent = sendErr || "Tap a platform to leave it out. Grey ones can't take replies (not signed in, or read only).";
    }
    cHint.classList.toggle("bad", !!sendErr);
    input.maxLength = limitFor(st);
    input.placeholder = replyTo ? "Reply to " + (replyTo.author || "") : "Message everyone";
    input.disabled = K.editing;
    updateSend();
    return true;
  }

  // the result card: "Sent to 2 of 3", a chip per platform, the reasons,
  // Retry the failed ones, Dismiss (on every screen)
  function drawResult(st) {
    var r = st.lastSend;
    res.textContent = "";
    res.hidden = !r || hiddenRes[r.id];
    if (res.hidden) return;
    var failed = (r.results || []).filter(function (x) { return !x.ok; });
    res.classList.toggle("allok", !failed.length);
    res.classList.toggle("compact", resCompact);
    res.title = resCompact ? failed.map(function (x) { return x.error || ""; }).join("\n") : "";
    if (!failed.length && okTimer !== r.id) {
      okTimer = r.id;
      setTimeout(function () { hiddenRes[r.id] = true; K.draw(); }, 6000);
    }
    var top = K.el("div", "rtop");
    top.appendChild(K.el("b", "rtitle", (resCompact ? "Sent " : "Sent to ") + r.sent + " of " + r.of));
    var rc = K.el("div", "rchips");
    (r.results || []).forEach(function (x) {
      if (resCompact && x.ok && failed.length) return; // one line: only what failed
      rc.appendChild(K.el("span", "rchip " + (x.ok ? "ok" : "bad"), (x.ok ? "✓ " : "✕ ") + (resCompact ? SHORT[x.platform] || plat(x.platform).name : plat(x.platform).name)));
    });
    top.appendChild(rc);
    res.appendChild(top);
    if (!resCompact) failed.forEach(function (x) {
      var e = K.el("div", "rerr", x.error || plat(x.platform).name + ": didn't go through");
      e.title = e.textContent;
      res.appendChild(e);
    });
    var a = K.el("div", "racts");
    if (failed.length) {
      var rb = JK.button({ label: resCompact ? "Retry" : "Retry " + failed.map(function (x) { return plat(x.platform).name; }).join(" + "),
        title: "Send it again to " + failed.map(function (x) { return plat(x.platform).name; }).join(" and "),
        onClick: function () { return K.act("retry_send", { sendId: r.id }).then(function () { K.draw(); }, function () {}); } });
      rb.classList.add("rretry");
      a.appendChild(rb);
    }
    var dis = JK.button({ label: resCompact ? "✕" : "Dismiss", title: "Dismiss", onClick: function () { hiddenRes[r.id] = true; K.draw(); return K.act("dismiss_send", { sendId: r.id }).catch(function () {}); } });
    dis.classList.add("rdismiss");
    a.appendChild(dis);
    if (resCompact) top.appendChild(a); else res.appendChild(a);
    // one line with no room for the chips: the title and Retry say enough
    if (resCompact && rc.scrollWidth > rc.clientWidth + 1) rc.style.visibility = "hidden";
  }

  // ---------------------------------------------------------- the pinned
  function drawPin(st) {
    var p = st.pinned;
    pin.textContent = "";
    pin.hidden = !p;
    if (!p) { pinOpen = false; return; }
    pin.classList.toggle("open", pinOpen);
    pin.appendChild(K.el("div", "pinlabel", "📌 PINNED" + (p.onScreen ? " · ON SCREEN" : "")));
    var t = K.el("div", "pintext");
    t.appendChild(K.el("b", "", p.author || ""));
    t.appendChild(document.createTextNode(" — " + (p.text || "") + (p.super ? " (" + p.super + ")" : "")));
    pin.appendChild(t);
    if (!pinOpen) return;
    var a = K.el("div", "acts");
    if (p.onScreen) {
      // another screen may have put it up: clearing from here needs this
      // widget's vMix too (else only the other screen can)
      if (canShow()) a.appendChild(JK.button({ label: "Clear from screen", onClick: function () { return clearScreen(); } }));
    } else {
      if (canShow()) {
        var show = JK.button({ label: "Show on screen", tone: "accent", onClick: function () { return showOnScreen(p); } });
        show.classList.add("show");
        a.appendChild(show);
      }
      a.appendChild(JK.button({ label: "Unpin", onClick: function () { return act("unpin"); } }));
    }
    if (a.childNodes.length) pin.appendChild(a);
  }

  // -------------------------------------------------------------- render
  K.render = function (st, set) {
    st = st || {};
    settings = set || {};
    var w = main.clientWidth, h = main.clientHeight + 26;
    var right = [];
    if (st.watching != null && w >= 300) right.push(st.watching + " watching");
    right.push((st.total || 0) + " msgs");
    var boxSmall = window.innerHeight < 260 || window.innerWidth < 220;
    overlay = boxSmall;
    compact = w < 300 || overlay;
    resCompact = h < 300;
    hdrText.textContent = right.join(" · ");
    hdr.set({ title: "Live chat", status: st.bad ? "bad" : st.live ? "ok" : "warn", source: st.source || "", note: st.note || "", bad: !!st.bad, right: hdrRight });
    document.body.classList.toggle("short", h < 240);
    document.body.classList.toggle("narrow", w < 300);
    document.body.classList.toggle("tall", h >= 420 && !overlay);
    errs.classList.remove("tight");
    pin.classList.remove("tight");
    chipsShort = 0;
    drawChips(st, w);
    st.messages = Array.isArray(st.messages) ? st.messages : [];
    lastSt = st;
    var composing = drawComposer(st);
    drawResult(st);
    drawPin(st);
    var hidden = st.hiddenCount || 0;
    unhide.set({ label: "Show " + hidden + " hidden" });
    unhide.hidden = !hidden;
    footNote.textContent = st.sample
      ? "Sample chat: add a YouTube, Facebook, Twitch, Rumble or Owncast chat connection."
      : composing ? "" : "To reply from here: sign in under Settings → Connections (YouTube, Twitch), or see Facebook and Owncast there.";
    foot.hidden = !footNote.textContent && !hidden;

    // the list keeps room for a few lines; else, one step at a time: the
    // chips drop their labels, the result card goes to one line, the
    // composer moves over the widget (header button), the error lines and
    // the pin go to one line, the chips to short names, the footer goes,
    // then the error lines (the chips still say which platform is wrong),
    // and last the chips
    var steps = [
      function () { chipsShort = 1; drawChips(st, w); },
      function () { if (!res.hidden && !resCompact) { resCompact = true; drawResult(st); } },
      function () { if (!overlay && !comp.hidden) { overlay = compact = true; composing = drawComposer(st); } },
      function () { errs.classList.add("tight"); pin.classList.add("tight"); },
      function () { chipsShort = 2; drawChips(st, w); },
      function () { foot.hidden = true; },
      function () { errs.hidden = true; },
      function () { chips.hidden = true; } // the header's dot still says something is wrong
    ];
    for (var i = 0; i < steps.length && wrap.clientHeight < MIN_LIST; i++) steps[i]();
    if (wrap.clientHeight < 40 && !res.hidden) res.hidden = true; // no room: the ✓ / ✕ under the message stay
    if (!overlay) ovlOpen = false;
    ovlBtn.hidden = !(overlay && composing);

    var msgs = Array.isArray(st.messages) ? st.messages : [];
    var newest = msgs.length ? msgs[msgs.length - 1].id : "";
    var keep = list.scrollTop, wasStuck = stick;
    if (open && !msgs.some(function (m) { return m.id === open; })) open = null;
    list.textContent = "";
    if (!msgs.length) {
      list.appendChild(K.el("div", "jk-empty", st.connected === false ? (st.error || "Not connected") : st.note || "Waiting for chat messages…"));
    } else {
      msgs.forEach(function (m) { list.appendChild(row(m, st)); });
    }
    if (wasStuck) toBottom(); else list.scrollTop = keep;
    // scrolled up: a way back down, saying so when new ones came in
    if (!wasStuck && newest !== lastSeen && lastSeen) jump.set({ label: "↓ New messages" });
    jump.hidden = wasStuck;
    lastSeen = newest;
  };

  // ------------------------------------------------------- taps, keys
  function toggle(id) {
    open = open === id ? null : id;
    K.draw();
    var r = list.querySelector('.msg[data-id="' + (window.CSS && CSS.escape ? CSS.escape(id) : id) + '"]');
    if (r && open) {
      var gap = r.offsetTop + r.offsetHeight - (list.scrollTop + list.clientHeight);
      if (gap > 0) list.scrollTop += gap + 4;
      if (r.offsetTop < list.scrollTop) list.scrollTop = r.offsetTop;
      onScroll();
    }
  }
  list.addEventListener("click", function (e) {
    if (scroll.moved() || K.editing || e.target.closest(".jk-btn")) return;
    var r = e.target.closest(".msg");
    if (r) toggle(r.dataset.id);
  });
  list.addEventListener("keydown", function (e) {
    var r = e.target.closest && e.target.closest(".msg");
    if (r && e.target === r && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(r.dataset.id); }
  });
  pin.addEventListener("click", function (e) {
    if (K.editing || e.target.closest(".jk-btn")) return;
    pinOpen = !pinOpen;
    K.draw();
  });

  K.start();
})();
