// The frame bridge (design/WIDGETS-V2.md in jadeck, bridge v1): init /
// state / size / theme from the host, actions and logs back. The page
// sets Kit.render(state, settings) and calls Kit.start(). Every control
// comes from jadeck's shared frame kit, window.JK (/api/kit/v1/kit.js,
// loaded before this file).
(function () {
  "use strict";
  var JK = window.JK, host = window.parent, seq = 0, pending = {};
  var K = window.Kit = { state: {}, settings: {}, size: { w: 0, h: 0 }, editing: false, touch: false, render: function () {}, busy: function () { return false; } };

  function send(m) { host.postMessage(m, "*"); }
  if (!JK) { // a jadeck from before the shared frame kit
    K.start = function () {
      document.body.textContent = "This widget needs a newer jadeck (0.6 or later).";
      document.body.style.cssText = "color:#8d96a5;font:12px system-ui,sans-serif;display:flex;align-items:center;justify-content:center;text-align:center;height:100%;margin:0;padding:8px";
      send({ t: "ready" });
    };
    return;
  }
  function req(m) {
    var id = "r" + (++seq);
    m.id = id;
    return new Promise(function (ok, bad) {
      pending[id] = { ok: ok, bad: bad };
      send(m);
      setTimeout(function () { if (pending[id]) { delete pending[id]; bad(new Error("no answer")); } }, 8000);
    });
  }
  K.act = function (name, params) {
    if (K.editing) return Promise.resolve(null);
    return req({ t: "action", name: name, params: params || {} }).catch(function (e) { K.log(name + ": " + e.message); throw e; });
  };
  K.log = function (text) { send({ t: "log", text: String(text).slice(0, 300) }); };
  K.el = function (tag, cls, text) { return JK.el(tag, cls, null, text); };

  var later = false;
  // draw: never under a finger (a hold, or the list being dragged).
  function draw() {
    if (JK.holds > 0 || K.busy()) { later = true; return; }
    later = false;
    JK.setEditing(K.editing);
    try { K.render(K.state, K.settings); } catch (e) { K.log("render: " + e.message); }
  }
  K.draw = draw;
  K.idle = function () { if (later) setTimeout(draw, 30); };
  JK.onIdle = K.idle;

  window.addEventListener("message", function (e) {
    if (e.source !== host) return;
    var m = e.data;
    if (!m || typeof m.t !== "string") return;
    switch (m.t) {
      case "init":
        K.state = m.state || {}; K.settings = m.settings || {}; K.size = m.size || K.size; K.editing = !!m.editing; K.touch = !!m.touch;
        JK.theme(m.theme); draw(); break;
      case "state": K.state = m.state || {}; draw(); break;
      case "size": K.size = m.size || K.size; draw(); break;
      case "theme": JK.theme(m.theme); break;
      case "result": {
        var p = pending[m.id];
        if (!p) return;
        delete pending[m.id];
        if (m.ok) p.ok(m.result); else p.bad(new Error(m.error || "failed"));
      }
    }
  });
  window.addEventListener("resize", draw);
  K.start = function () { send({ t: "ready" }); };
})();
