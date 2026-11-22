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

  // scroller: the list moves by drag (touch or mouse) and wheel, never a
  // scrollbar. onScroll runs after each move. s.busy(): a drag is under
  // way (don't redraw); s.moved(): the last press was a drag, not a tap.
  C.scroller = function (list, onScroll) {
    var drag = null, moved = false;
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
    list.addEventListener("keydown", function (e) {
      if (e.key === "PageUp" || e.key === "PageDown") { e.preventDefault(); list.scrollTop += (e.key === "PageUp" ? -1 : 1) * list.clientHeight * 0.8; onScroll(); }
    });
    return { busy: function () { return !!(drag && moved); }, moved: function () { return moved; } };
  };
})();
