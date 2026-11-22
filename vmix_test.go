package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// vmixFake records vMix Web Controller calls.
type vmixFake struct {
	mu    sync.Mutex
	calls []string // "Function SelectedName=Value" (Input checked)
	input string
	fail  string // a SelectedName vMix rejects
	auth  string // "user:pass" wanted
}

func (f *vmixFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" && r.URL.Path != "/api" {
			t.Errorf("path %s", r.URL.Path)
		}
		if f.auth != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u+":"+p != f.auth {
				w.WriteHeader(401)
				return
			}
		}
		q := r.URL.Query()
		if q.Get("Function") == "" {
			w.Write([]byte(`<vmix><version>29.0.0.50</version></vmix>`))
			return
		}
		if q.Get("Input") != f.input {
			t.Errorf("Input %q, want %q", q.Get("Input"), f.input)
		}
		f.mu.Lock()
		f.calls = append(f.calls, q.Get("Function")+" "+q.Get("SelectedName")+"="+q.Get("Value"))
		f.mu.Unlock()
		if q.Get("SelectedName") == f.fail {
			w.WriteHeader(500)
			w.Write([]byte("Input not found"))
			return
		}
		w.Write([]byte("Function completed successfully."))
	})
}

func testVmix(t *testing.T, f *vmixFake, input string) *vmixClient {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	f.input = input
	v, err := newVmix(srv.URL, input, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVmixShowSetsTheTitleFields(t *testing.T) {
	f := &vmixFake{}
	v := testVmix(t, f, "Social 1")
	m := Message{ID: "youtube:m2", Author: "Pedro M.", Text: "For the fund & more", Super: "$10.00", Avatar: "https://yt3.example/p.jpg"}
	if err := v.show(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"SetText Title.Text=Live Comments",
		"SetText FromName.Text=Pedro M. · $10.00",
		"SetText Message.Text=For the fund & more",
		"SetImage FromPhoto.Source=https://yt3.example/p.jpg",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	// no picture: no SetImage
	f.calls = nil
	if err := v.show(context.Background(), Message{Author: "Ana L.", Text: "Hola"}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("calls %v", f.calls)
	}
	f.calls = nil
	if err := v.clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, "|") != "SetText FromName.Text=|SetText Message.Text=|SetImage FromPhoto.Source=" {
		t.Fatalf("clear %v", f.calls)
	}
}

func TestVmixErrors(t *testing.T) {
	f := &vmixFake{fail: "Message.Text"}
	v := testVmix(t, f, "Social 1")
	err := v.show(context.Background(), Message{Author: "A", Text: "B"})
	if err == nil || !strings.Contains(err.Error(), "Input not found") {
		t.Fatalf("vMix error: %v", err)
	}
	f.auth = "admin:secret"
	if err := v.ping(context.Background()); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Fatalf("auth: %v", err)
	}
	v.user, v.pass = "admin", "secret"
	if err := v.ping(context.Background()); err != nil {
		t.Fatalf("with auth: %v", err)
	}
	none, _ := newVmix("", "", "", "", "")
	if err := none.show(context.Background(), Message{Text: "x"}); err != errNoTitle {
		t.Fatalf("no title input: %v", err)
	}
	dead, _ := newVmix("127.0.0.1:1", "Social 1", "", "", "")
	if err := dead.show(context.Background(), Message{Text: "x"}); err == nil || !strings.Contains(err.Error(), "no answer from vMix") {
		t.Fatalf("dead vMix: %v", err)
	}
}

func TestVmixBase(t *testing.T) {
	for in, want := range map[string]string{
		"": "http://127.0.0.1:8088", "127.0.0.1:8088": "http://127.0.0.1:8088", "office-pc": "http://office-pc:8088",
		"http://192.168.1.30:8088/": "http://192.168.1.30:8088", "100.64.0.5": "http://100.64.0.5:8088",
	} {
		if got, err := vmixBase(in); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := vmixBase("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
}
