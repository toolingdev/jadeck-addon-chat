package main

// Small HTTP helpers shared by the platform clients. Errors never carry
// a URL: the Facebook token travels in the query string, and Go's
// *url.Error prints the whole URL.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var defaultClient = &http.Client{Timeout: 15 * time.Second}

// apiError is an error a platform answered with.
type apiError struct {
	Status  int
	Code    int    // Facebook's error code (190 = bad token)
	Reason  string // YouTube's errors[0].reason (quotaExceeded, liveChatEnded…)
	Message string
}

func (e *apiError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Reason != "" {
		return fmt.Sprintf("%s (%s)", msg, e.Reason)
	}
	return msg
}

// getJSON GETs u with the given headers and decodes a 2xx JSON body into
// out. A non-2xx body is handed to bad, which turns it into an error.
func getJSON(ctx context.Context, hc *http.Client, u string, hdr http.Header, out any, bad func(status int, body []byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errors.New("bad request address")
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return scrub(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return scrub(err)
	}
	if res.StatusCode/100 != 2 {
		return bad(res.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unreadable answer: %v", err)
	}
	return nil
}

// scrub drops the URL from a transport error.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("no answer (timed out)")
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errors.New(err.Error())
}

// redact removes secrets from text (a platform could echo them back).
func redact(s string, secrets ...string) string {
	for _, x := range secrets {
		if len(x) >= 6 {
			s = strings.ReplaceAll(s, x, "••••")
		}
	}
	return s
}

// sleep waits d or until ctx ends; false when ctx ended.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
