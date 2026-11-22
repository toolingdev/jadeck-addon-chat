package main

// A simulated chat: played by a Live chat connection while no platform
// connection is open (so the widget can be tried and placed before the
// keys exist), for the Settings preview, and in tests. Names are made up (first name and an
// initial).

import (
	"context"
	"fmt"
	"time"
)

type fakeLine struct {
	platform, author, text, super string
}

// fakeScript is the simulated chat, in order; it loops.
var fakeScript = []fakeLine{
	{YouTube, "Ruth K.", "Good morning from Denver!", ""},
	{YouTube, "Sam W.", "Watching from Phoenix, blessings everyone!", ""},
	{Facebook, "Ana L.", "¿Pueden subir el volumen un poco? 🙏", ""},
	{YouTube, "Pedro M.", "For the building fund — God bless!", "$10.00"},
	{Facebook, "José T.", "Amén! Gracias por la palabra de hoy", ""},
	{Facebook, "Maria G.", "Dios les bendiga en abundancia 🙏", ""},
	{Twitch, "grace_h", "The sound is great today", ""},
	{Facebook, "Luis P.", "Saludos desde San Antonio", ""},
	{Rumble, "DavidN", "Blessings from Ohio", "$2.00"},
	{Owncast, "Carmen V.", "Here with the whole family", ""},
	{YouTube, "Nora B.", "Can you share the verse again?", ""},
	{Facebook, "Elena R.", "Thank you for streaming every week", ""},
	{YouTube, "Tom C.", "Praying for everyone here", "$5.00"},
	{Twitch, "lily_s", "Hello from my kitchen ☕", "100 bits"},
}

// fake plays fakeScript: `backlog` lines at once, then one every `every`.
type fake struct {
	start   time.Time
	every   time.Duration
	backlog int
	viewers int
	gen     int // a fresh id range each time the sample chat comes back
	primed  bool
}

func newFake() *fake {
	return &fake{start: time.Now(), every: 7 * time.Second, backlog: 5, viewers: 34}
}

func (f *fake) platform() string { return "sample" }

// fakeMessage is the n-th simulated message (n from 0), posted at `at`.
func fakeMessage(n int, at time.Time) Message { return fakeMessageGen(0, n, at) }

func fakeMessageGen(gen, n int, at time.Time) Message {
	l := fakeScript[n%len(fakeScript)]
	id := fmt.Sprintf("%s%d", samplePrefix, n)
	if gen > 1 {
		id = fmt.Sprintf("%sg%d:%d", samplePrefix, gen, n)
	}
	return Message{ID: id, Platform: l.platform, Author: l.author, Text: l.text, Super: l.super, At: at}
}

// prime posts the status and the backlog at once (run does it unless
// prime was called first).
func (f *fake) prime(out sink) {
	f.primed = true
	out.status(sourceStatus{Platform: "sample", State: stLive, Viewers: f.viewers, Title: "Sample chat"})
	var first []Message
	for i := 0; i < f.backlog; i++ {
		first = append(first, fakeMessageGen(f.gen, i, f.start.Add(-time.Duration(f.backlog-i)*time.Minute)))
	}
	out.add(first...)
}

func (f *fake) run(ctx context.Context, out sink) {
	if !f.primed {
		f.prime(out)
	}
	for n := f.backlog; ; n++ {
		if !sleep(ctx, f.every) {
			return
		}
		out.add(fakeMessageGen(f.gen, n, time.Now()))
		if n%4 == 0 {
			f.viewers += []int{1, -1, 2, 0}[n/4%4]
			out.status(sourceStatus{Platform: "sample", State: stLive, Viewers: f.viewers, Title: "Sample chat"})
		}
	}
}
