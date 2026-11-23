// What both Live chat widgets share (comments.js and chat.js): the
// platforms' names and colours, the badge, the time, a platform's problem
// lines, and a list that scrolls by drag / wheel without a scrollbar.
// Controls come from jadeck's shared frame kit (JK); this is only the
// chat's own look.
(function () {
  "use strict";
  var K = window.Kit, JK = window.JK;
  var C = window.Chat = {};
  if (!JK) return;

  C.PLAT = {
    youtube: { cls: "yt", name: "YouTube", letter: "" },
    facebook: { cls: "fb", name: "Facebook", letter: "f" },
    twitch: { cls: "tw", name: "Twitch", letter: "t" },
    rumble: { cls: "rb", name: "Rumble", letter: "R" },
    owncast: { cls: "oc", name: "Owncast", letter: "O" },
    sample: { cls: "sm", name: "Sample chat", letter: "S" },
    you: { cls: "me", name: "You", letter: "↩" }
  };
  C.SHORT = { youtube: "YT", facebook: "FB", twitch: "TW", owncast: "OC", rumble: "RB" };
  C.plat = function (p) { return C.PLAT[p] || { cls: "sm", name: p || "", letter: "?" }; };
  C.badge = function (p) {
    var d = C.plat(p), b = K.el("i", "badge " + d.cls);
    b.setAttribute("aria-label", d.name);
    if (d.letter) b.textContent = d.letter;
    return b;
  };
  C.hm = function (iso) {
    var d = new Date(iso);
    if (isNaN(d)) return "";
    var h = d.getHours() % 12 || 12, m = d.getMinutes();
    return h + ":" + (m < 10 ? "0" : "") + m;
  };
  C.superLabel = function (m) {
    if (m.super === "Sub") return "SUB";
    if (/bits$/.test(m.super)) return m.super.toUpperCase();
    if (m.platform === "rumble") return "RANT " + m.super;
    return "SUPER CHAT " + m.super;
  };

  // errLines: what's wrong, one line per platform with a problem, in full
  // words so it can be acted on (+ extra lines, e.g. a vMix error).
  C.errLines = function (errs, sources, extra) {
    errs.textContent = "";
    var bad = (sources || []).filter(function (s) { return s.state === "error" && s.note; });
    bad.forEach(function (s) {
      var d = C.plat(s.platform), n = (s.note || "").replace(/^(YouTube|Facebook|Twitch|Rumble|Owncast)( chat)?: /, "");
      var line = K.el("div", "err", n.indexOf(d.name) === 0 ? n : d.name + ": " + n);
      line.title = line.textContent;
      errs.appendChild(line);
    });
    (extra || []).forEach(function (t) {
      if (!t) return;
      var line = K.el("div", "err", t);
      line.title = t;
      errs.appendChild(line);
    });
    errs.hidden = !errs.childNodes.length;
  };

  // scroller: the list scrolls natively on touch (the phone's own flick and
  // momentum) and by mouse drag, wheel and Page keys, never a visible
  // scrollbar. onScroll runs after each move. s.busy(): a scroll is under
  // way (don't redraw); s.moved(): the last press was a drag, not a tap.
  // fillRows: puts one row per message in list, in order, reusing the
  // row already there when nothing it shows changed (sig(m) the same; a
  // null sig always rebuilds). Rows aren't torn down on every update, so a
  // finger or a glide on the list isn't cut off by a new comment.
  C.fillRows = function (list, msgs, build, sig) {
    var old = list._rows || {}, rows = {}, want = [];
    msgs.forEach(function (m) {
      var s = sig(m), o = old[m.id], el;
      el = o && s !== null && o.sig === s && o.el.parentNode === list ? o.el : build(m);
      rows[m.id] = { sig: s, el: el };
      want.push(el);
    });
    list._rows = rows;
    var cur = list.firstChild;
    want.forEach(function (el) {
      if (el === cur) cur = cur.nextSibling;
      else list.insertBefore(el, cur);
    });
    while (cur) { var next = cur.nextSibling; list.removeChild(cur); cur = next; }
  };
  C.clearRows = function (list) { list._rows = {}; list.textContent = ""; };

  C.scroller = function (list, onScroll) {
    var drag = null, moved = false, lastScroll = 0, settle = 0;
    function release() { moved = false; K.idle(); }
    list.addEventListener("pointerdown", function (e) {
      if (e.button || e.target.closest(".jk-btn")) return;
      drag = { y: e.clientY, top: list.scrollTop, id: e.pointerId, touch: e.pointerType !== "mouse" };
      moved = false;
    });
    list.addEventListener("pointermove", function (e) {
      if (!drag) return;
      var dy = e.clientY - drag.y;
      if (!moved && Math.abs(dy) > 6) {
        moved = true;
        if (!drag.touch) { try { list.setPointerCapture(drag.id); } catch (x) { /* fine */ } }
      }
      if (moved && !drag.touch) list.scrollTop = drag.top - dy; // touch: the browser scrolls
    });
    function endDrag() {
      if (!drag) return;
      var touch = drag.touch;
      drag = null;
      // a flick keeps gliding after the finger lifts: the scroll listener
      // ends it; a mouse drag ends here
      if (moved && !touch) setTimeout(release, 0);
    }
    list.addEventListener("pointerup", endDrag);
    list.addEventListener("pointercancel", endDrag);
    list.addEventListener("lostpointercapture", endDrag);
    list.addEventListener("scroll", function () {
      lastScroll = Date.now();
      onScroll();
      clearTimeout(settle);
      settle = setTimeout(function () { if (!drag && moved) release(); else if (!drag) K.idle(); }, 180);
    }, { passive: true });
    list.addEventListener("keydown", function (e) {
      if (e.key === "PageUp" || e.key === "PageDown") { e.preventDefault(); list.scrollTop += (e.key === "PageUp" ? -1 : 1) * list.clientHeight * 0.8; }
    });
    return {
      // a finger on the list (even before it moves) or a glide still going:
      // a redraw now would pull rows out from under the gesture
      busy: function () { return !!drag || Date.now() - lastScroll < 250; },
      moved: function () { return moved; }
    };
  };
})();
