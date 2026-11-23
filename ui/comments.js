// Live chat (comments only), for the person up front: the widget's
// platforms (setting "platforms") in one list, big and easy to read,
// newest at the bottom (it follows new messages unless you've scrolled
// up). Each comment: "YOUTUBE · Ruth K." in the platform's colour, then
// the words. The pinned / on-screen one sits on top in amber. No reply
// box and no buttons: tapping does nothing unless the setting "Let people
// tap comments" is on, and then a tap offers Pin and Hide here.
// Header and buttons are the shared frame kit's (JK); the list scrolls by
// drag / wheel without a scrollbar (common.js).
(function () {
  "use strict";
  var K = window.Kit, JK = window.JK, C = window.Chat, main = document.getElementById("main");
  if (!JK) { K.start(); return; }

  var hdr = JK.header({ title: "Live chat" });
  var errs = K.el("div", "errs");
  var pin = K.el("div", "pin jk-live"), wrap = K.el("div", "listwrap"), list = K.el("div", "list jk-live"), foot = K.el("div", "foot");
  var footNote = K.el("span", "footnote");
  var jump = JK.button({ label: "↓ Newest", onClick: function () { stick = true; toBottom(); } });
  jump.classList.add("jump");
  var unhide = JK.button({ label: "Show hidden", onClick: function () { return K.act("show_hidden"); } });
  unhide.classList.add("unhide");
  wrap.appendChild(list); wrap.appendChild(jump);
  foot.appendChild(footNote); foot.appendChild(unhide);
  main.appendChild(errs); main.appendChild(pin); main.appendChild(wrap); main.appendChild(foot);

  var open = null, pinOpen = false, stick = true, lastSeen = "", tap = false, narrow = false, ack = false, newOnly = false, hdrTight = false;
  var plat = C.plat, MIN_LIST = 72;
  var scroll = C.scroller(list, onScroll);
  K.busy = scroll.busy;

  function bottomGap() { return list.scrollHeight - list.clientHeight - list.scrollTop; }
  function toBottom() { list.scrollTop = list.scrollHeight; jump.hidden = true; jump.set({ label: "↓ Newest" }); }
  function onScroll() {
    stick = bottomGap() <= 4;
    jump.hidden = stick;
    if (stick) jump.set({ label: "↓ Newest" });
  }
  function act(name, params) {
    return K.act(name, params).then(function () { open = null; pinOpen = false; K.draw(); }, function () {});
  }

  // "YOUTUBE · Ruth K." (a Super Chat adds its amount; one sent from
  // jadeck is "YOU · <account>")
  function meta(m) {
    var d = K.el("div", "meta");
    if (m.mine) {
      d.appendChild(K.el("span", "who me", "YOU · " + (m.author || "")));
      return d;
    }
    var p = plat(m.platform);
    d.appendChild(K.el("span", "pname " + p.cls, p.name.toUpperCase()));
    d.appendChild(K.el("span", "who " + p.cls, " · " + (m.author || "")));
    // narrow: the amount alone ("$5.00", "100 BITS"), so it isn't cut
    if (m.super) d.appendChild(K.el("span", "amt", " · " + (narrow ? m.super.toUpperCase() : C.superLabel(m))));
    return d;
  }
  function row(m) {
    var canAck = ack && !m.mine;
    var r = K.el("div", "msg" + (m.super ? " super" : "") + (m.mine ? " mine" : "") + (m.id === open ? " open" : "") +
      (canAck ? (m.addressed ? " done" : " fresh") : ""));
    r.dataset.id = m.id;
    if (tap) { r.tabIndex = 0; r.setAttribute("aria-expanded", m.id === open ? "true" : "false"); }
    var body = K.el("div", "body");
    var md = meta(m);
    if (canAck) md.appendChild(K.el("span", m.addressed ? "ackd" : "newtag", m.addressed ? " · ✓ ADDRESSED" : " · NEW"));
    body.appendChild(md);
    body.appendChild(K.el("div", "text", m.text || ""));
    r.appendChild(body);
    if (canAck) {
      // ✓ ticks it off (on every screen); on one already ticked it undoes it
      var ok = JK.button({ label: "✓", title: m.addressed ? "Mark as new again" : "Mark addressed",
        onClick: function () { return K.act("comment_addressed", { id: m.id, addressed: !m.addressed }).then(function () {}, function () {}); } });
      ok.classList.add("ack");
      ok.setAttribute("aria-label", m.addressed ? "Mark as new again: " + (m.author || "") : "Mark addressed: " + (m.author || ""));
      r.appendChild(ok);
    }
    if (tap && m.id === open) {
      var a = K.el("div", "acts");
      a.appendChild(JK.button({ label: "Pin", onClick: function () { return act("pin", { id: m.id }); } }));
      a.appendChild(JK.button({ label: "Hide here", onClick: function () { return act("hide", { id: m.id }); } }));
      r.appendChild(a);
    }
    return r;
  }

  function drawPin(st) {
    var p = st.pinned;
    pin.textContent = "";
    pin.hidden = !p;
    if (!p) { pinOpen = false; return; }
    pin.classList.toggle("open", pinOpen && tap);
    pin.appendChild(K.el("div", "pinlabel", "📌 PINNED" + (p.onScreen ? " · ON SCREEN" : "")));
    var t = K.el("div", "pintext");
    t.appendChild(K.el("b", "", p.author || ""));
    t.appendChild(document.createTextNode(" — " + (p.text || "") + (p.super ? " (" + p.super + ")" : "")));
    pin.appendChild(t);
    // the one on screen stays pinned until the booth clears it
    if (!tap || !pinOpen || p.onScreen) return;
    var a = K.el("div", "acts");
    a.appendChild(JK.button({ label: "Unpin", onClick: function () { return act("unpin"); } }));
    pin.appendChild(a);
  }

  // the header: "3 PLATFORMS · 61 WATCHING" (one platform: its name)
  function source(st) {
    var srcs = Array.isArray(st.sources) ? st.sources : [];
    var parts = [];
    if (st.sample) parts.push("Sample chat");
    else if (srcs.length > 1) {
      var names = {};
      srcs.forEach(function (s) { names[s.platform] = true; });
      var n = Object.keys(names).length;
      parts.push(n > 1 ? n + " platforms" : plat(srcs[0].platform).name);
    } else if (srcs.length) parts.push(plat(srcs[0].platform).name);
    if (st.watching != null) parts.push(st.watching + " watching");
    return parts.join(" · ");
  }

  K.render = function (st, settings) {
    st = st || {};
    settings = settings || {};
    tap = settings.tap === true || settings.tap === "true";
    if (!tap) { open = null; pinOpen = false; }
    document.body.classList.toggle("tappable", tap);
    var w = main.clientWidth, h = main.clientHeight + 26;
    document.body.classList.toggle("short", h < 240);
    narrow = w < 300;
    hdrTight = w < 440; // phone-wide: the tick-off count and switch take the source's place
    document.body.classList.toggle("narrow", narrow);
    document.body.classList.toggle("small", w < 260 || h < 220);
    var srcs = Array.isArray(st.sources) ? st.sources : [];
    ack = settings.ack === true || settings.ack === "true";
    if (!ack) newOnly = false;
    // ticking off on a phone: the count and switch need the header's room
    hdr.set({ title: "Live chat", status: st.bad ? "bad" : st.live ? "ok" : "warn", source: ack && hdrTight ? "" : source(st), note: "", bad: !!st.bad, right: ack ? ackRight(st) : "" });
    hdr.el.title = srcs.map(function (s) {
      var d = plat(s.platform);
      return d.name + (s.label ? " (" + s.label + ")" : "") + ": " + (s.state === "live" ? "live" + (s.viewers >= 0 ? ", " + s.viewers + " watching" : "") : s.state || "") + (s.note ? " — " + s.note : "");
    }).join("\n");
    C.errLines(errs, srcs, []);
    drawPin(st);

    var msgs = Array.isArray(st.messages) ? st.messages : [];
    if (ack && newOnly) msgs = msgs.filter(function (m) { return m.mine || !m.addressed; });
    var newest = msgs.length ? msgs[msgs.length - 1].id : "";
    var keep = list.scrollTop, wasStuck = stick;
    if (open && !msgs.some(function (m) { return m.id === open; })) open = null;
    list.textContent = "";
    if (!msgs.length) {
      list.appendChild(K.el("div", "jk-empty", st.connected === false ? (st.error || "Not connected") : st.note || "Waiting for comments…"));
    } else {
      msgs.forEach(function (m) { list.appendChild(row(m)); });
    }
    if (wasStuck) toBottom(); else list.scrollTop = keep;
    if (!wasStuck && newest !== lastSeen && lastSeen) jump.set({ label: "↓ New comments" });
    jump.hidden = wasStuck;
    lastSeen = newest;

    var hidden = tap ? st.hiddenCount || 0 : 0;
    unhide.set({ label: "Show " + hidden + " hidden" });
    unhide.hidden = !hidden;
    footNote.textContent = st.sample ? "Sample chat: add a YouTube, Facebook, Twitch, Rumble or Owncast chat connection." : "";
    foot.hidden = !footNote.textContent && !hidden;
    // a short box keeps room for the list: the error lines go to one
    // line, the pin to one line, then the error lines go (the header's
    // tooltip still has them)
    errs.classList.remove("tight");
    pin.classList.remove("tight");
    if (wrap.clientHeight < MIN_LIST && !errs.hidden) errs.classList.add("tight");
    if (wrap.clientHeight < MIN_LIST && !pin.hidden) pin.classList.add("tight");
    if (wrap.clientHeight < MIN_LIST) errs.hidden = true;
    if (wasStuck) toBottom();
  };

  // the header, ticking comments off: "2 NEW" and the New only switch
  function ackRight(st) {
    var box = K.el("span", "ackhead");
    var n = st.newCount || 0;
    box.appendChild(K.el("span", "count" + (n ? " some" : ""), n + " NEW"));
    var b = document.createElement("button");
    b.type = "button";
    b.className = "newonly" + (newOnly ? " on" : "");
    b.setAttribute("aria-pressed", newOnly ? "true" : "false");
    b.textContent = narrow ? "New" : "New only";
    b.title = "Show only the comments not ticked off yet";
    b.addEventListener("click", function () { if (K.editing) return; newOnly = !newOnly; stick = true; K.draw(); });
    box.appendChild(b);
    return box;
  }

  // ------------------------------------------------------- taps, keys
  function toggle(id) {
    if (!tap) return;
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
    if (!tap || scroll.moved() || K.editing || e.target.closest(".jk-btn")) return;
    var r = e.target.closest(".msg");
    if (r) toggle(r.dataset.id);
  });
  list.addEventListener("keydown", function (e) {
    var r = e.target.closest && e.target.closest(".msg");
    if (tap && r && e.target === r && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); toggle(r.dataset.id); }
  });
  pin.addEventListener("click", function (e) {
    if (!tap || K.editing || e.target.closest(".jk-btn")) return;
    pinOpen = !pinOpen;
    K.draw();
  });

  K.start();
})();
