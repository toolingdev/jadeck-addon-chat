package main

// Shared bits of the reply sign-ins: form and JSON POSTs that never put a
// token in an error, the device-code state an action field polls, and a
// lock per sign-in so two sends never refresh one token at once (Twitch
// refresh tokens work once).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// postForm POSTs an url-encoded form and returns the status and body.
func postForm(ctx context.Context, hc *http.Client, u string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, errors.New("bad request address")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return do(hc, req)
}

// postJSON POSTs a JSON body with the given headers.
func postJSON(ctx context.Context, hc *http.Client, u string, hdr http.Header, in any) (int, []byte, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return 0, nil, errors.New("bad request address")
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return do(hc, req)
}

func do(hc *http.Client, req *http.Request) (int, []byte, error) {
	res, err := hc.Do(req)
	if err != nil {
		return 0, nil, scrub(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, scrub(err)
	}
	return res.StatusCode, body, nil
}

// bearerGet GETs JSON with "Authorization: Bearer".
func bearerGet(ctx context.Context, hc *http.Client, u, token string, extra http.Header, out any) (int, error) {
	hdr := http.Header{"Authorization": {"Bearer " + token}}
	for k, v := range extra {
		hdr[k] = v
	}
	status := 0
	err := getJSON(ctx, hc, u, hdr, out, func(st int, body []byte) error {
		status = st
		return &apiError{Status: st, Message: redact(strings.TrimSpace(string(body)), token)}
	})
	if err == nil {
		status = 200
	}
	return status, err
}

// device is a device-code sign-in in progress: the code to type, where,
// and when to ask again.
type device struct {
	deviceCode string
	userCode   string
	verifyURL  string
	expires    time.Time
	interval   time.Duration
	next       time.Time
}

// waitResult is the action field while the person signs in.
func (d *device) waitResult(where string) addon.ActionFieldResult {
	mins := int(time.Until(d.expires).Minutes())
	host := strings.TrimPrefix(strings.TrimPrefix(d.verifyURL, "https://"), "http://")
	if i := strings.IndexAny(host, "?#"); i >= 0 {
		host = host[:i]
	}
	return addon.ActionFieldResult{
		Text: fmt.Sprintf("On any phone or computer, open %s and type this code (%s). It's good for %d min.", host, where, max(mins, 1)),
		Code: d.userCode, URL: d.verifyURL, Poll: true, Button: "Waiting…",
	}
}

// signParams is what the controller sends to an action field's action.
type signParams struct {
	Status bool `json:"status"`
	Poll   bool `json:"poll"`
}

// asMap turns an ActionFieldResult into the action's result object.
func asMap(r addon.ActionFieldResult) map[string]any {
	b, _ := json.Marshal(r)
	var m map[string]any
	json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

var (
	keyLocksMu sync.Mutex
	keyLocks   = map[string]*sync.Mutex{}
)

// keyLock is the lock for one sign-in (refreshing it, saving it).
func keyLock(key string) *sync.Mutex {
	keyLocksMu.Lock()
	defer keyLocksMu.Unlock()
	m := keyLocks[key]
	if m == nil {
		m = &sync.Mutex{}
		keyLocks[key] = m
	}
	return m
}
