package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/toolingdev/jadeck/sdk/addon"
	"github.com/toolingdev/jadeck/sdk/addon/addontest"
)

// No platform set up: the sample chat, over the pipe the way jadeck runs it.
func TestSampleChatOverThePipe(t *testing.T) {
	h := addontest.Start(t, spec())
	caps := h.Open("c", "chat", map[string]any{})
	if len(caps) != 0 {
		t.Fatalf("caps %v", caps)
	}
	st := h.State("c", nil)
	if st["sample"] != true || st["source"] != "SAMPLE CHAT" || st["live"] != true {
		t.Fatalf("state %v", st)
	}
	msgs, _ := st["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("%d sample messages", len(msgs))
	}
	first := msgs[0].(map[string]any)
	id := first["id"].(string)
	if first["platform"] != YouTube || first["author"] == "" || first["at"] == "" {
		t.Fatalf("message %v", first)
	}
	if d, _ := st["detail"].(string); !strings.Contains(d, "sample chat") || strings.Contains(d, "vMix") {
		t.Fatalf("detail %q", d)
	}
	for _, k := range []string{"canShow", "screenLabel", "vmixError"} {
		if _, ok := st[k]; ok {
			t.Errorf("state still has %s", k)
		}
	}
	// the old hub vMix actions are gone
	for _, a := range []string{"show_on_screen", "clear_screen"} {
		if err := h.TryCall("c", "addon.action", map[string]any{"name": a, "params": map[string]any{"id": id}}, nil); err == nil {
			t.Errorf("%s still runs", a)
		}
	}
	// pin and hide are the widget's own, shared by every screen
	h.Call("c", "addon.action", map[string]any{"name": "pin", "params": map[string]any{"id": id}}, nil)
	st = h.State("c", nil)
	if p, _ := st["pinned"].(map[string]any); p == nil || p["id"] != id || p["onScreen"] == true {
		t.Fatalf("pinned %v", st["pinned"])
	}
	second := msgs[1].(map[string]any)["id"].(string)
	h.Call("c", "addon.action", map[string]any{"name": "hide", "params": map[string]any{"id": second}}, nil)
	st = h.State("c", nil)
	if st["hiddenCount"].(float64) != 1 || len(st["messages"].([]any)) != 3 {
		t.Fatalf("after hide: hidden %v, %d shown", st["hiddenCount"], len(st["messages"].([]any)))
	}
	h.Call("c", "addon.action", map[string]any{"name": "show_hidden"}, nil)
	h.Call("c", "addon.action", map[string]any{"name": "unpin"}, nil)
	if st = h.State("c", nil); st["pinned"] != nil || st["hiddenCount"].(float64) != 0 {
		t.Fatalf("after unpin / show hidden: %v %v", st["pinned"], st["hiddenCount"])
	}
	if err := h.TryCall("c", "addon.action", map[string]any{"name": "pin", "params": map[string]any{"id": "nope"}}, nil); err == nil {
		t.Fatal("pinned a message that isn't there")
	}
	if err := h.TryCall("c", "addon.action", map[string]any{"name": "dance"}, nil); err == nil {
		t.Fatal("unknown action ran")
	}
}

// The reply widget fills the vMix title itself (through the vMix
// connection), then marks the message here so every screen sees it.
func TestMarkAndClearOnScreen(t *testing.T) {
	h := addontest.Start(t, spec())
	h.Open("c", "chat", map[string]any{})
	st := h.State("c", nil)
	msgs := st["messages"].([]any)
	id := msgs[len(msgs)-1].(map[string]any)["id"].(string)
	other := msgs[0].(map[string]any)["id"].(string)

	if err := h.TryCall("c", "addon.action", map[string]any{"name": "mark_on_screen", "params": map[string]any{}}, nil); err == nil {
		t.Fatal("marked without an id")
	}
	if err := h.TryCall("c", "addon.action", map[string]any{"name": "mark_on_screen", "params": map[string]any{"id": "nope"}}, nil); err == nil {
		t.Fatal("marked a message that isn't there")
	}
	var res map[string]any
	h.Call("c", "addon.action", map[string]any{"name": "mark_on_screen", "params": map[string]any{"id": id}}, &res)
	if res["id"] != id {
		t.Fatalf("result %v", res)
	}
	// every screen: the plain state and a widget's filtered one
	for _, set := range []map[string]any{nil, {"platforms": []any{}}} {
		st = h.State("c", set)
		p, _ := st["pinned"].(map[string]any)
		if p == nil || p["id"] != id || p["onScreen"] != true {
			t.Fatalf("settings %v: pinned %v", set, st["pinned"])
		}
	}
	// unpin keeps the one on screen pinned; pinning another moves the pin
	h.Call("c", "addon.action", map[string]any{"name": "unpin"}, nil)
	if p, _ := h.State("c", nil)["pinned"].(map[string]any); p == nil || p["id"] != id {
		t.Fatalf("unpin took the on-screen pin: %v", p)
	}
	h.Call("c", "addon.action", map[string]any{"name": "pin", "params": map[string]any{"id": other}}, nil)
	if p, _ := h.State("c", nil)["pinned"].(map[string]any); p == nil || p["id"] != other || p["onScreen"] == true {
		t.Fatalf("pin another: %v", p)
	}
	h.Call("c", "addon.action", map[string]any{"name": "mark_on_screen", "params": map[string]any{"id": id}}, nil)
	h.Call("c", "addon.action", map[string]any{"name": "clear_on_screen"}, nil)
	if st = h.State("c", nil); st["pinned"] != nil {
		t.Fatalf("still pinned after clear: %v", st["pinned"])
	}
	// clearing twice is fine
	h.Call("c", "addon.action", map[string]any{"name": "clear_on_screen"}, nil)
}

// Each widget picks its platforms: messages, chips and totals follow
// (AddonStateFor through the pipe, with the widget's settings).
func TestWidgetPlatformFilter(t *testing.T) {
	h := addontest.Start(t, spec())
	h.Open("c", "chat", map[string]any{})
	platformsOf := func(st map[string]any) map[string]int {
		out := map[string]int{}
		for _, m := range st["messages"].([]any) {
			out[m.(map[string]any)["platform"].(string)]++
		}
		return out
	}
	all := h.State("c", nil)
	if got := platformsOf(all); got[YouTube] == 0 || got[Facebook] == 0 {
		t.Fatalf("sample platforms %v", got)
	}
	// a widget with no pick (or an unknown one) sees everything
	for _, set := range []map[string]any{{}, {"platforms": []any{}}, {"platforms": []any{"myspace"}}, {"platforms": ""}} {
		if st := h.State("c", set); len(st["messages"].([]any)) != len(all["messages"].([]any)) || st["total"] != all["total"] {
			t.Errorf("settings %v: %d messages, total %v", set, len(st["messages"].([]any)), st["total"])
		}
	}
	yt := h.State("c", map[string]any{"platforms": []any{"youtube"}})
	if got := platformsOf(yt); len(got) != 1 || got[YouTube] == 0 {
		t.Fatalf("YouTube only: %v", got)
	}
	if yt["total"].(float64) != float64(platformsOf(all)[YouTube]) {
		t.Fatalf("YouTube total %v", yt["total"])
	}
	both := h.State("c", map[string]any{"platforms": "facebook, YouTube"})
	if got := platformsOf(both); len(got) != 2 || got[Twitch] != 0 {
		t.Fatalf("comma list: %v", got)
	}
	// a pin on another platform isn't this widget's
	fb := h.State("c", map[string]any{"platforms": []any{"facebook"}})
	fbID := fb["messages"].([]any)[0].(map[string]any)["id"].(string)
	h.Call("c", "addon.action", map[string]any{"name": "pin", "params": map[string]any{"id": fbID}}, nil)
	if st := h.State("c", map[string]any{"platforms": []any{"youtube"}}); st["pinned"] != nil {
		t.Fatalf("YouTube widget shows a Facebook pin: %v", st["pinned"])
	}
	if st := h.State("c", map[string]any{"platforms": []any{"facebook"}}); st["pinned"] == nil {
		t.Fatal("Facebook widget lost its pin")
	}
}

func TestOpenRejectsHalfSetUpPlatforms(t *testing.T) {
	sp := spec()
	for _, c := range []struct{ typ, cfg string }{
		{"youtube", `{"youtubeKey":"k"}`},
		{"youtube", `{"youtubeChannel":"@SomeChannel"}`},
		{"youtube", `{"youtubeKey":"k","youtubeChannel":"not a channel"}`},
		{"facebook", `{"facebookPage":"123"}`},
		{"facebook", `{"facebookToken":"t"}`},
		{"twitch", `{}`},
		{"twitch", `{"channel":"no spaces allowed"}`},
		{"rumble", `{}`},
		{"rumble", `{"apiUrl":"http://rumble.com/-livestream-api/get-data?key=x"}`},
		{"rumble", `{"apiUrl":"https://evil.example/-livestream-api/get-data?key=x"}`},
		{"owncast", `{}`},
		{"owncast", `{"server":"ftp://x"}`},
	} {
		if conn, err := sp.Open[c.typ]([]byte(c.cfg)); err == nil {
			conn.(interface{ Close() error }).Close()
			t.Errorf("%s %s opened", c.typ, c.cfg)
		}
	}
	// no network in tests: check the poll floor without starting sources
	if f := (youtubeConfig{Poll: "1"}).pollFloor(); f != 3e9 {
		t.Fatalf("poll floor %v", f)
	}
	if f := (youtubeConfig{Poll: 12.0}).pollFloor(); f != 12e9 {
		t.Fatalf("poll floor %v", f)
	}
	if f := (youtubeConfig{}).pollFloor(); f != 8e9 {
		t.Fatalf("default poll floor %v", f)
	}
}

// Every connection type validates; the platform ones have an opener, a
// Needs line and a guide that points at the widgets; the hub is Auto,
// with no fields; both widgets' fields check out.
func TestManifestValidatesWithGuides(t *testing.T) {
	sp := spec()
	m := sp.Manifest.LowestAPIVersion()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"chat", "youtube", "facebook", "twitch", "rumble", "owncast"}
	if len(m.ConnectionTypes) != len(want) {
		t.Fatalf("%d connection types", len(m.ConnectionTypes))
	}
	for i, ct := range m.ConnectionTypes {
		if ct.ID != want[i] || sp.Open[ct.ID] == nil {
			t.Errorf("type %d: %s (opener %v)", i, ct.ID, sp.Open[ct.ID] != nil)
		}
		if ct.ID == "chat" {
			if !ct.Auto || len(ct.Fields) != 0 || ct.Needs != "" || ct.Guide != nil {
				t.Errorf("hub: auto %v, %d fields, needs %q, guide %v", ct.Auto, len(ct.Fields), ct.Needs, ct.Guide)
			}
			continue
		}
		if ct.Auto {
			t.Errorf("%s is auto", ct.ID)
		}
		if ct.Needs == "" || ct.Guide == nil || len(ct.Guide.Steps) == 0 || ct.Guide.Note == "" {
			t.Errorf("%s: needs %q, guide %+v", ct.ID, ct.Needs, ct.Guide)
		}
		if !slices.Contains(ct.Guide.Steps, widgetStep) {
			t.Errorf("%s: the guide doesn't say to add a widget", ct.ID)
		}
		if ct.Sample == nil || ct.SampleTest == "" {
			t.Errorf("%s: no sample", ct.ID)
		}
		for _, f := range ct.Fields {
			if len(f.Help) > 80 {
				t.Errorf("%s.%s: help is %d characters; the guide carries the steps", ct.ID, f.Key, len(f.Help))
			}
		}
	}

	// the Auto rule and optionsConn are really checked
	bad := m
	bad.ConnectionTypes = slices.Clone(m.ConnectionTypes)
	bad.ConnectionTypes[0].Fields = []addon.Field{{Key: "vmixHost", Label: "vMix address", Type: "text"}}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "auto") {
		t.Errorf("an auto type with fields: %v", err)
	}
	bad = m
	bad.Widgets = slices.Clone(m.Widgets)
	bad.Widgets[1].Fields = slices.Clone(m.Widgets[1].Fields)
	for i, f := range bad.Widgets[1].Fields {
		if f.Key == "vmixTitle" {
			bad.Widgets[1].Fields[i].OptionsConn = "platforms"
		}
	}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "optionsConn") {
		t.Errorf("optionsConn on a non-connection field: %v", err)
	}

	// the two widgets
	if len(m.Widgets) != 2 || m.Widgets[0].Type != "live_chat" || m.Widgets[1].Type != "live_chat_reply" {
		t.Fatalf("widgets %+v", m.Widgets)
	}
	fields := func(w addon.WidgetDef) map[string]addon.Field {
		out := map[string]addon.Field{}
		for _, f := range w.Fields {
			out[f.Key] = f
		}
		return out
	}
	for _, w := range m.Widgets {
		f := fields(w)["platforms"]
		if f.Type != addon.FieldSelect || !f.Multiple || len(f.Options) != 5 {
			t.Errorf("%s: platforms %+v", w.Type, f)
		}
		for _, o := range f.Options {
			if platformNames[o.Value] != o.Label {
				t.Errorf("%s: option %+v isn't a platform id", w.Type, o)
			}
		}
		if w.MinPx == nil || w.MinPx.W != 220 || w.MinPx.H != 180 {
			t.Errorf("%s: minPx %+v", w.Type, w.MinPx)
		}
	}
	lc := fields(m.Widgets[0])
	if lc["tap"].Type != addon.FieldBool || lc["tap"].Default != false || lc["tap"].Label != "Let people tap comments" || lc["ack"].Type != addon.FieldBool || lc["ack"].Default != false || len(lc) != 3 {
		t.Errorf("live_chat fields %+v", lc)
	}
	rf := fields(m.Widgets[1])
	if v := rf["vmix"]; v.Type != addon.FieldConnection || v.Addon != "vmix" {
		t.Errorf("vmix field %+v", v)
	}
	if v := rf["vmixTitle"]; v.Type != addon.FieldSelect || v.OptionsFrom != "vmix.titles" || v.OptionsConn != "vmix" {
		t.Errorf("vmixTitle field %+v", v)
	}
	if v := rf["vmixHeading"]; v.Default != "Live Comments" || !v.Advanced {
		t.Errorf("vmixHeading field %+v", v)
	}
	if m.Widgets[0].Entry == m.Widgets[1].Entry {
		t.Error("both widgets share one page")
	}
}

// Nothing in the manifest (types, fields, actions, samples, texts) still
// talks about the hub's old vMix settings or actions.
func TestNoHubVmixLeftovers(t *testing.T) {
	b, err := json.Marshal(spec().Manifest)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, old := range []string{"vmixHost", "vmixUser", "vmixPassword", "vmixTitleText", "vMix address", "vMix title input",
		"show_on_screen", "clear_screen", "screenLabel", "canShow", "Live chat connection", "Add this first", "8088"} {
		if strings.Contains(js, old) {
			t.Errorf("manifest still mentions %q", old)
		}
	}
	for _, a := range spec().Manifest.Actions {
		if strings.Contains(a.ID, "screen") && a.MacroStep {
			t.Errorf("%s is a macro step (the vMix part lives in the widget)", a.ID)
		}
	}
	ids := map[string]bool{}
	for _, a := range spec().Manifest.Actions {
		ids[a.ID] = true
	}
	for _, want := range []string{"pin", "unpin", "hide", "show_hidden", "send_message", "retry_send", "dismiss_send", "mark_on_screen", "clear_on_screen", "reply_signin"} {
		if !ids[want] {
			t.Errorf("action %s missing", want)
		}
	}
}

func TestSamplesMatchTheWidgets(t *testing.T) {
	for name, st := range map[string]map[string]any{"comments": commentsSample(), "reply": replySample()} {
		for _, k := range []string{"messages", "pinned", "watching", "total", "source", "sources", "hiddenCount"} {
			if _, ok := st[k]; !ok {
				t.Errorf("%s sample has no %s", name, k)
			}
		}
	}
	c := commentsSample()
	for _, k := range []string{"canSend", "lastSend"} {
		if _, ok := c[k]; ok {
			t.Errorf("comments sample has composer data %s", k)
		}
	}
	for _, m := range c["messages"].([]wireMessage) {
		if m.Mine || len(m.Sent) > 0 {
			t.Errorf("comments sample has a sent message %+v", m)
		}
	}
	r := replySample()
	if r["canSend"] != true || r["pinned"].(wireMessage).OnScreen != true {
		t.Errorf("reply sample %v %v", r["canSend"], r["pinned"])
	}
}

// The speaker ticks comments off: they're marked on every screen and
// leave the NEW count; unticking brings them back.
func TestCommentAddressed(t *testing.T) {
	h := addontest.Start(t, spec())
	h.Open("c", "chat", map[string]any{})
	st := h.State("c", nil)
	msgs := st["messages"].([]any)
	before := st["newCount"].(float64)
	id := msgs[len(msgs)-1].(map[string]any)["id"].(string)
	if err := h.TryCall("c", "addon.action", map[string]any{"name": "comment_addressed", "params": map[string]any{"id": "nope"}}, nil); err == nil {
		t.Fatal("addressed a message that isn't there")
	}
	h.Call("c", "addon.action", map[string]any{"name": "comment_addressed", "params": map[string]any{"id": id}}, nil)
	st = h.State("c", map[string]any{"platforms": []any{}})
	last := st["messages"].([]any)
	if m := last[len(last)-1].(map[string]any); m["id"] != id || m["addressed"] != true || st["newCount"].(float64) != before-1 {
		t.Fatalf("after ✓: %v, new %v (was %v)", m, st["newCount"], before)
	}
	h.Call("c", "addon.action", map[string]any{"name": "comment_addressed", "params": map[string]any{"id": id, "addressed": false}}, nil)
	if st = h.State("c", nil); st["newCount"].(float64) != before {
		t.Fatalf("untick: new %v", st["newCount"])
	}
}
