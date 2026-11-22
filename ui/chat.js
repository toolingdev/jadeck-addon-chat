// Live chat: every platform connection (YouTube, Facebook, Twitch,
// Rumble, Owncast) in one list, newest at the bottom (it follows new
// messages unless you've scrolled up), Super Chats / bits / subs / rants
// in green, the pinned / on-screen message in amber on top, and one
// status chip per platform under the header. Tap a message for Show
// on screen, Pin and Hide here. Header, buttons and status dot are the
// shared frame kit's (JK); the list scrolls by drag / wheel without a
// scrollbar.
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
  main.appendChild(chips); main.appendChild(errs); main.appendChild(pin); main.appendChild(wrap); main.appendChild(foot);

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
    sample: { cls: "sm", name: "Sample chat", letter: "S" }
  };
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
      if (s.label && s.platform !== "sample" && w >= 300) name += " " + s.label;
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
    var r = K.el("div", "msg" + (m.super ? " super" : "") + (m.id === open ? " open" : ""));
    r.dataset.id = m.id;
    r.tabIndex = 0;
    r.setAttribute("aria-expanded", m.id === open ? "true" : "false");
    r.appendChild(badge(m.platform));
    var body = K.el("div", "body");
    var meta = K.el("div", "meta");
    if (m.super) meta.appendChild(K.el("span", "amt", superLabel(m) + " · "));
    meta.appendChild(K.el("span", "who " + plat(m.platform).cls, m.author || ""));
    meta.appendChild(document.createTextNode(" · " + hm(m.at)));
    body.appendChild(meta);
    body.appendChild(K.el("div", "text", m.text || ""));
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
  function actions(m, st) {
    var a = K.el("div", "acts");
    var show = JK.button({ label: "Show on screen", tone: "accent", disabled: !st.canShow,
      title: st.canShow ? "Put this message in the vMix title" : "Set the vMix title input in this connection's settings",
      onClick: function () { return act("show_on_screen", { id: m.id }); } });
    show.classList.add("show");
    a.appendChild(show);
    a.appendChild(JK.button({ label: "Pin", onClick: function () { return act("pin", { id: m.id }); } }));
    a.appendChild(JK.button({ label: "Hide here", onClick: function () { return act("hide", { id: m.id }); } }));
    return a;
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
    hdr.set({ title: "Live chat", status: st.bad ? "bad" : st.live ? "ok" : "warn", source: st.source || "", note: st.note || "", bad: !!st.bad, right: right.join(" · ") });
    document.body.classList.toggle("short", h < 240);
    document.body.classList.toggle("narrow", w < 300);
    drawChips(st, w);

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
      : "Read-only: replying comes in a later version (it needs each platform's sign-in).";
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
