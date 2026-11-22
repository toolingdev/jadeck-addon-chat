package main

import (
	"context"
	"errors"
)

// A source feeds one platform's chat into the add-on.
type source interface {
	platform() string
	run(ctx context.Context, out sink)
}

// sink is where a source sends what it learns.
type sink interface {
	add(msgs ...Message)
	remove(id string)
	status(st sourceStatus)
}

// Source states.
const (
	stLive    = "live"    // a live stream with chat: polling it
	stWaiting = "waiting" // nothing live right now: checking every so often
	stError   = "error"   // a setup or platform problem (Note says what)
)

// sourceStatus is how one platform is doing.
type sourceStatus struct {
	Platform string
	State    string
	Note     string // plain words for the widget
	Viewers  int    // watching now; -1 unknown
	Title    string // the live stream's title
	Label    string // the channel / page / handle, when the source learns it ("" keeps the one from the settings)
	Info     bool   // Note is plain information while live, not a problem
}

// errNotLive: nothing is live right now (not a problem).
var errNotLive = errors.New("nothing live right now")
