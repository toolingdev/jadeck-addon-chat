// Live chat: every platform connection (YouTube, Facebook, Twitch,
// Rumble, Owncast) in one list, newest at the bottom (it follows new
// messages unless you've scrolled up), Super Chats / bits / subs / rants
// in green, the pinned / on-screen message in amber on top, and one
// status chip per platform under the header. Tap a message for Reply,
// Show on screen, Pin and Hide here. The composer at the bottom sends to
// every platform that can reply (tap a chip to leave one out) or replies
// to one message; the result card says what got through. In a small box
// the composer opens over the widget from a Reply button in the header.
// Header, buttons and status dot are the shared frame kit's (JK); the
// list scrolls by drag / wheel without a scrollbar.
(function () {
  "use strict";
  var K = window.Kit, JK = window.JK, main = document.getElementById("main");
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

  var open = null, pinOpen = false, stick = true, lastSeen = "", drag = null, moved = false;
  K.busy = function () { return !!(drag && moved); };

  function hm(iso) {
    var d = new Date(iso);
    if (isNaN(d)) return "";
    var h = d.getHours() % 12 || 12, m = d.getMinutes();
    return h + ":" + (m < 10 ? "0" : "") + m;
  }
  var PLAT = {
    youtube: { cls: "yt", name: "YouTube", letter: "" },
    facebook: { cls: "fb", name: "Facebook", letter: "f" },
    twitch: { cls: "tw", name: "Twitch", letter: "t" },
    rumble: { cls: "rb", name: "Rumble", letter: "R" },
    owncast: { cls: "oc", name: "Owncast", letter: "O" },
    sample: { cls: "sm", name: "Sample chat", letter: "S" },
    you: { cls: "me", name: "You", letter: "↩" }
  };
  var SHORT = { youtube: "YT", facebook: "FB", twitch: "TW", owncast: "OC", rumble: "RB" };
  function plat(p) { return PLAT[p] || { cls: "sm", name: p || "", letter: "?" }; }
  function badge(p) {
    var d = plat(p), b = K.el("i", "badge " + d.cls);
    b.setAttribute("aria-label", d.name);
    if (d.letter) b.textContent = d.letter;
    return b;
  }

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
      var name = d.name;
      if (s.label && s.platform !== "sample" && w >= 300 && !chipsShort) name += " " + s.label;
      c.appendChild(K.el("span", "cname", name));
      var tail = "";
      if (s.state === "live") tail = s.viewers != null && s.viewers >= 0 ? String(s.viewers) : "";
      else if (s.state === "error") tail = "";
      else tail = "waiting";
      if (tail) c.appendChild(K.el("span", "ctail", tail));
      c.title = d.name + (s.label ? " (" + s.label + ")" : "") + ": " +
        (s.state === "live" ? "live" + (s.viewers >= 0 ? ", " + s.viewers + " watching" : "") : s.state || "") +
        (s.note ? " — " + s.note : "");
      chips.appendChild(c);
    });
    // what's wrong, one line per platform with a problem (the chip only
    // says which), in full words so it can be acted on
    errs.textContent = "";
    var bad = list.filter(function (s) { return s.state === "error" && s.note; });
    errs.hidden = !bad.length;
    bad.forEach(function (s) {
      var d = plat(s.platform), n = (s.note || "").replace(/^(YouTube|Facebook|Twitch|Rumble|Owncast)( chat)?: /, "");
      var line = K.el("div", "err", n.indexOf(d.name) === 0 ? n : d.name + ": " + n);
      line.title = line.textContent;
      errs.appendChild(line);
    });
  }
  function act(name, params) {
    return K.act(name, params).then(function () { open = null; pinOpen = false; K.draw(); }, function () {});
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
  function superLabel(m) {
    if (m.super === "Sub") return "SUB";
    if (/bits$/.test(m.super)) return m.super.toUpperCase();
    if (m.platform === "rumble") return "RANT " + m.super;
    return "SUPER CHAT " + m.super;
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
    var show = JK.button({ label: "Show on screen", tone: "accent", disabled: !st.canShow,
      title: st.canShow ? "Put this message in the vMix title" : "Set the vMix title input in this connection's settings",
      onClick: function () { return act("show_on_screen", { id: m.id }); } });
    show.classList.add("show");
    a.appendChild(show);
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
  var MIN_LIST = 76, chipsShort = false;
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
  }

  // ---------------------------------------------------------- the pinned
  function drawPin(st) {
    var p = st.pinned;
    pin.textContent = "";
    pin.hidden = !p;
    if (!p) { pinOpen = false; return; }
    pin.classList.toggle("open", pinOpen);
    pin.appendChild(K.el("div", "pinlabel", "📌 PINNED" + (p.onScreen ? " · " + (st.screenLabel || "on screen") : "")));
    var t = K.el("div", "pintext");
    t.appendChild(K.el("b", "", p.author || ""));
    t.appendChild(document.createTextNode(" — " + (p.text || "") + (p.super ? " (" + p.super + ")" : "")));
    pin.appendChild(t);
    if (!pinOpen) return;
    var a = K.el("div", "acts");
    if (p.onScreen) {
      a.appendChild(JK.button({ label: "Take off screen", onClick: function () { return act("clear_screen"); } }));
    } else {
      var show = JK.button({ label: "Show on screen", tone: "accent", disabled: !st.canShow, onClick: function () { return act("show_on_screen", { id: p.id }); } });
      show.classList.add("show");
      a.appendChild(show);
      a.appendChild(JK.button({ label: "Unpin", onClick: function () { return act("unpin"); } }));
    }
    pin.appendChild(a);
  }

  // -------------------------------------------------------------- render
  K.render = function (st) {
    st = st || {};
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
    chipsShort = false;
    drawChips(st, w);
    st.messages = Array.isArray(st.messages) ? st.messages : [];
    lastSt = st;
    var composing = drawComposer(st);
    drawResult(st);
    // the list keeps room for a few lines: else the result card goes to
    // one line, then the composer moves over the widget (header button)
    if (wrap.clientHeight < MIN_LIST && w >= 300) { chipsShort = true; drawChips(st, w); }
    if (wrap.clientHeight < MIN_LIST && !res.hidden && !resCompact) { resCompact = true; drawResult(st); }
    if (wrap.clientHeight < MIN_LIST && !overlay && !comp.hidden) { overlay = compact = true; composing = drawComposer(st); }
    if (wrap.clientHeight < 40 && !res.hidden) res.hidden = true; // no room: the ✓ / ✕ under the message stay
    if (!overlay) ovlOpen = false;
    ovlBtn.hidden = !(overlay && composing);

    drawPin(st);

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

    var hidden = st.hiddenCount || 0;
    unhide.set({ label: "Show " + hidden + " hidden" });
    unhide.hidden = !hidden;
    footNote.textContent = st.sample
      ? "Sample chat: add a YouTube, Facebook, Twitch, Rumble or Owncast chat connection."
      : composing ? "" : "To reply from here: sign in under Settings → Connections (YouTube, Twitch), or see Facebook and Owncast there.";
    foot.hidden = !footNote.textContent && !hidden;
  };

  // ------------------------------------------------- touch, wheel, keys
  list.addEventListener("pointerdown", function (e) {
    if (e.button || e.target.closest(".jk-btn")) return;
    drag = { y: e.clientY, top: list.scrollTop, id: e.pointerId };
    moved = false;
  });
  list.addEventListener("pointermove", function (e) {
    if (!drag) return;
    var dy = e.clientY - drag.y;
    if (!moved && Math.abs(dy) > 6) {
      moved = true;
      try { list.setPointerCapture(drag.id); } catch (x) { /* fine */ }
    }
    if (moved) { list.scrollTop = drag.top - dy; onScroll(); }
  });
  function endDrag() {
    if (!drag) return;
    drag = null;
    if (moved) setTimeout(function () { moved = false; K.idle(); }, 0);
  }
  list.addEventListener("pointerup", endDrag);
  list.addEventListener("pointercancel", endDrag);
  list.addEventListener("lostpointercapture", endDrag);
  list.addEventListener("wheel", function (e) {
    var dy = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaMode === 2 ? e.deltaY * list.clientHeight : e.deltaY;
    var before = list.scrollTop;
    list.scrollTop += dy;
    if (list.scrollTop !== before) { e.preventDefault(); onScroll(); }
  }, { passive: false });
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
    if (moved || K.editing || e.target.closest(".jk-btn")) return;
    var r = e.target.closest(".msg");
    if (r) toggle(r.dataset.id);
  });
  list.addEventListener("keydown", function (e) {
    var r = e.target.closest && e.target.closest(".msg");
    if (r && e.target === r && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(r.dataset.id); }
    if (e.key === "PageUp" || e.key === "PageDown") { e.preventDefault(); list.scrollTop += (e.key === "PageUp" ? -1 : 1) * list.clientHeight * 0.8; onScroll(); }
  });
  pin.addEventListener("click", function (e) {
    if (K.editing || e.target.closest(".jk-btn")) return;
    pinOpen = !pinOpen;
    K.draw();
  });

  K.start();
})();
