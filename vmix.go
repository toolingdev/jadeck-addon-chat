package main

// Putting a message on screen: vMix's Web Controller API
// (GET http://<vmix>:8088/api/?Function=SetText&Input=…&SelectedName=…&Value=…)
// fills a GT title made in vMix Social's layout:
//   Title.Text     the heading ("Live Comments")
//   FromName.Text  who wrote it
//   Message.Text   what they wrote
//   FromPhoto.Source  their picture (SetImage), when the platform gives one
// Bringing the title on air (an overlay) stays in vMix or a jadeck vMix
// widget / macro.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The title's fields (vMix Social's format).
const (
	fieldTitle = "Title.Text"
	fieldName  = "FromName.Text"
	fieldText  = "Message.Text"
	fieldPhoto = "FromPhoto.Source"
)

type vmixClient struct {
	base       string // http://host:port
	input      string // the title input: name, number or key
	title      string // Title.Text
	user, pass string
	hc         *http.Client
}

// vmixBase turns "127.0.0.1:8088", "pc-name" or "http://host:8088/" into
// http://host:port.
func vmixBase(addr string) (string, error) {
	a := strings.TrimSpace(addr)
	if a == "" {
		a = "127.0.0.1:8088"
	}
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	u, err := url.Parse(a)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("vMix: %q isn't an address (like 127.0.0.1:8088)", addr)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "8088")
	}
	return u.Scheme + "://" + host, nil
}

func newVmix(addr, input, title, user, pass string) (*vmixClient, error) {
	base, err := vmixBase(addr)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" {
		title = "Live Comments"
	}
	return &vmixClient{base: base, input: strings.TrimSpace(input), title: title, user: user, pass: pass,
		hc: &http.Client{Timeout: 5 * time.Second}}, nil
}

var errNoTitle = errors.New("set the vMix title input in this connection's settings first")

// call runs one vMix function.
func (v *vmixClient) call(ctx context.Context, q url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.base+"/api/?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	if v.user != "" || v.pass != "" {
		req.SetBasicAuth(v.user, v.pass)
	}
	res, err := v.hc.Do(req)
	if err != nil {
		return fmt.Errorf("no answer from vMix at %s: %v", strings.TrimPrefix(v.base, "http://"), scrub(err))
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return errors.New("vMix wants the Web Controller username and password")
	case res.StatusCode/100 != 2:
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = res.Status
		}
		return fmt.Errorf("vMix said: %s", msg)
	}
	return nil
}

func (v *vmixClient) setText(ctx context.Context, field, value string) error {
	return v.call(ctx, url.Values{"Function": {"SetText"}, "Input": {v.input}, "SelectedName": {field}, "Value": {value}})
}

func (v *vmixClient) setImage(ctx context.Context, field, value string) error {
	return v.call(ctx, url.Values{"Function": {"SetImage"}, "Input": {v.input}, "SelectedName": {field}, "Value": {value}})
}

// show fills the title with a message.
func (v *vmixClient) show(ctx context.Context, m Message) error {
	if v.input == "" {
		return errNoTitle
	}
	name := m.Author
	if m.Super != "" {
		name += " · " + m.Super
	}
	for _, f := range [][2]string{{fieldTitle, v.title}, {fieldName, name}, {fieldText, m.Text}} {
		if err := v.setText(ctx, f[0], f[1]); err != nil {
			return err
		}
	}
	if m.Avatar != "" && (strings.HasPrefix(m.Avatar, "https://") || strings.HasPrefix(m.Avatar, "http://")) {
		// A title without a photo field says so; the text is up anyway.
		v.setImage(ctx, fieldPhoto, m.Avatar)
	}
	return nil
}

// clear empties the title's name, message and photo.
func (v *vmixClient) clear(ctx context.Context) error {
	if v.input == "" {
		return errNoTitle
	}
	for _, f := range []string{fieldName, fieldText} {
		if err := v.setText(ctx, f, ""); err != nil {
			return err
		}
	}
	v.setImage(ctx, fieldPhoto, "")
	return nil
}

// ping checks that vMix answers (GET /api is its state XML).
func (v *vmixClient) ping(ctx context.Context) error {
	return v.call(ctx, url.Values{})
}
