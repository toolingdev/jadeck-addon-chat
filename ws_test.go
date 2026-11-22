package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// wsPeer is the server end of a test websocket.
type wsPeer struct {
	c   net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

// wsAccept upgrades a request (test servers only).
func wsAccept(w http.ResponseWriter, r *http.Request) (*wsPeer, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" {
		http.Error(w, "not a websocket", 400)
		return nil, io.EOF
	}
	c, brw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	brw.Flush()
	return &wsPeer{c: c, br: brw.Reader}, nil
}

// send writes an unmasked frame (servers don't mask).
func (p *wsPeer) frame(op byte, payload []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	b := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		b = append(b, byte(n))
	case n <= 0xffff:
		b = append(b, 126, byte(n>>8), byte(n))
	default:
		b = append(b, 127)
		b = binary.BigEndian.AppendUint64(b, uint64(n))
	}
	_, err := p.c.Write(append(b, payload...))
	return err
}

func (p *wsPeer) send(s string) error { return p.frame(0x1, []byte(s)) }

// read returns the next client message (masked text); pongs come back as "\x00pong:<payload>".
func (p *wsPeer) read() (string, error) {
	var h [8]byte
	if _, err := io.ReadFull(p.br, h[:2]); err != nil {
		return "", err
	}
	op, n := h[0]&0x0f, uint64(h[1]&0x7f)
	if h[1]&0x80 == 0 {
		return "", io.ErrUnexpectedEOF // clients must mask
	}
	switch n {
	case 126:
		io.ReadFull(p.br, h[:2])
		n = uint64(binary.BigEndian.Uint16(h[:2]))
	case 127:
		io.ReadFull(p.br, h[:8])
		n = binary.BigEndian.Uint64(h[:8])
	}
	var mask [4]byte
	io.ReadFull(p.br, mask[:])
	b := make([]byte, n)
	if _, err := io.ReadFull(p.br, b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] ^= mask[i%4]
	}
	switch op {
	case 0x8:
		return "", io.EOF
	case 0xA:
		return "\x00pong:" + string(b), nil
	}
	return string(b), nil
}

func (p *wsPeer) close() { p.c.Close() }

func TestWebsocketClientRoundTrip(t *testing.T) {
	big := strings.Repeat("x", 70000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := wsAccept(w, r)
		if err != nil {
			return
		}
		defer p.close()
		msg, _ := p.read()
		p.frame(0x9, []byte("hi")) // a ping: the client answers while reading
		p.send("echo " + msg)
		if got, _ := p.read(); got != "\x00pong:hi" {
			t.Errorf("pong %q", got)
		}
		// a fragmented message, then a big one
		p.wmu.Lock()
		p.c.Write([]byte{0x01, 3, 'a', 'b', 'c'})
		p.c.Write([]byte{0x80, 3, 'd', 'e', 'f'})
		p.wmu.Unlock()
		p.send(big)
		p.frame(0x8, []byte{0x03, 0xe8})
		p.read()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := wsDial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?accessToken=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.send("hello")
	for _, want := range []string{"echo hello", "abcdef", big} {
		if got, err := ws.read(); err != nil || got != want {
			t.Fatalf("read %.20q %v, want %.20q", got, err, want)
		}
	}
	if _, err := ws.read(); err != errWSClosed {
		t.Fatalf("close: %v", err)
	}
}

func TestWebsocketDialErrorsHideTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 403) }))
	defer srv.Close()
	_, err := wsDial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?accessToken=secret123", nil)
	if err == nil || strings.Contains(err.Error(), "secret123") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("refused: %v", err)
	}
	_, err = wsDial(context.Background(), "ws://127.0.0.1:1/ws?accessToken=secret123", nil)
	if err == nil || strings.Contains(err.Error(), "secret123") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("dead server: %v", err)
	}
	// the dial ends with ctx
	ctx, cancel := context.WithCancel(context.Background())
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := wsAccept(w, r)
		if err == nil {
			defer p.close()
			p.read()
		}
	}))
	defer ok.Close()
	ws, err := wsDial(ctx, "ws"+strings.TrimPrefix(ok.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { _, err := ws.read(); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read after cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read didn't end with ctx")
	}
}
