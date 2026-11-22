package main

import (
	"net/http/httptest"
	"strings"
	"testing"

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
	if d, _ := st["detail"].(string); !strings.Contains(d, "sample chat") || !strings.Contains(d, "no title input") {
		t.Fatalf("detail %q", d)
	}

	// Show on screen needs the vMix title
	if err := h.TryCall("c", "addon.action", map[string]any{"name": "show_on_screen", "params": map[string]any{"id": id}}, nil); err == nil ||
		!strings.Contains(err.Error(), "vMix title input") {
		t.Fatalf("show without a title input: %v", err)
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

// Show on screen fills the vMix title and pins the message as on screen.
func TestShowOnScreenThroughVmix(t *testing.T) {
	f := &vmixFake{input: "Social 1"}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	h := addontest.Start(t, spec())
	h.Open("c", "chat", map[string]any{"vmixHost": srv.URL, "vmixTitle": "Social 1", "vmixTitleText": "Comments"})
	st := h.State("c", nil)
	if d := st["detail"].(string); !strings.Contains(d, "vMix: answered") {
		t.Fatalf("detail %q", d)
	}
	// no id: the newest message
	var res map[string]any
	h.Call("c", "addon.action", map[string]any{"name": "show_on_screen"}, &res)
	newest := res["id"].(string)
	st = h.State("c", nil)
	p, _ := st["pinned"].(map[string]any)
	if p == nil || p["id"] != newest || p["onScreen"] != true || st["screenLabel"] != "on screen in vMix (Social 1)" {
		t.Fatalf("pinned %v label %v", p, st["screenLabel"])
	}
	if f.calls[0] != "SetText Title.Text=Comments" || !strings.HasPrefix(f.calls[1], "SetText FromName.Text=") {
		t.Fatalf("vMix calls %v", f.calls)
	}
	h.Call("c", "addon.action", map[string]any{"name": "clear_screen"}, nil)
	if st = h.State("c", nil); st["pinned"] != nil {
		t.Fatalf("still pinned after clear: %v", st["pinned"])
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
		{"chat", `{"vmixHost":"ftp://x"}`},
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

// Every connection type validates, has an opener, a Needs line and a guide.
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
		if ct.Needs == "" || ct.Guide == nil || len(ct.Guide.Steps) == 0 || ct.Guide.Note == "" {
			t.Errorf("%s: needs %q, guide %+v", ct.ID, ct.Needs, ct.Guide)
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
}

func TestSampleStateMatchesTheWidget(t *testing.T) {
	st := sampleState()
	for _, k := range []string{"messages", "pinned", "watching", "total", "source", "sources", "screenLabel"} {
		if _, ok := st[k]; !ok {
			t.Errorf("sample has no %s", k)
		}
	}
}
