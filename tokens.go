package main

// Sign-in tokens for replies (YouTube, Twitch), kept across restarts in
// one JSON file only this user can read:
//
//	<user config dir>/jadeck-addon-chat/tokens.json   (dir 0700, file 0600)
//
// keyed by platform + a stable key (the channel and the app's client id).
// Writes go to a temp file in the same folder and are renamed over the
// old one, under a mutex. Tokens are never logged, never put in widget
// state, Test lines or errors.

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// tokenEntry is one sign-in.
type tokenEntry struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expiry  time.Time `json:"expiry"`
	// Who replies come from (shown as "Can reply as …").
	Account string `json:"account,omitempty"`
	UserID  string `json:"userId,omitempty"` // Twitch: the signed-in user's id (sender_id)
	// Twitch: the channel's broadcaster id.
	ChannelID string    `json:"channelId,omitempty"`
	Saved     time.Time `json:"saved"`
}

// usable: an access token that won't run out in the next minute.
func (e tokenEntry) usable(now time.Time) bool {
	return e.Access != "" && (e.Expiry.IsZero() || now.Add(time.Minute).Before(e.Expiry))
}

// tokenStore is the tokens file. Safe for concurrent use.
type tokenStore struct {
	mu    sync.Mutex
	path  string
	cache map[string]tokenEntry // the file as last read or written (only this process writes it)
}

// tokensPath is where tokens live (tests point it elsewhere).
var tokensPath = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no folder for sign-ins: %v", err)
	}
	return filepath.Join(dir, "jadeck-addon-chat", "tokens.json"), nil
}

var (
	tokensOnce   sync.Mutex
	sharedTokens *tokenStore
)

// tokens is the process's token store (one file, one mutex).
func tokens() *tokenStore {
	tokensOnce.Lock()
	defer tokensOnce.Unlock()
	if sharedTokens == nil {
		p, err := tokensPath()
		if err != nil {
			p = ""
		}
		sharedTokens = &tokenStore{path: p}
	}
	return sharedTokens
}

var errNoTokenFolder = errors.New("this computer has no folder to keep the sign-in in")

func (s *tokenStore) readLocked() (map[string]tokenEntry, error) {
	if s.cache != nil {
		return maps.Clone(s.cache), nil
	}
	out := map[string]tokenEntry{}
	if s.path == "" {
		return out, errNoTokenFolder
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, errors.New("couldn't read the saved sign-ins")
	}
	if len(b) > 0 && json.Unmarshal(b, &out) != nil {
		out = map[string]tokenEntry{} // a damaged file: start over
	}
	s.cache = maps.Clone(out)
	return out, nil
}

func (s *tokenStore) writeLocked(all map[string]tokenEntry) error {
	if s.path == "" {
		return errNoTokenFolder
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("couldn't make the folder for sign-ins")
	}
	os.Chmod(dir, 0o700)
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tokens-*.tmp")
	if err != nil {
		return errors.New("couldn't save the sign-in")
	}
	tmp := f.Name()
	defer os.Remove(tmp) // gone after the rename; cleans up on failure
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return errors.New("couldn't save the sign-in")
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return errors.New("couldn't save the sign-in")
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errors.New("couldn't save the sign-in")
	}
	if err := f.Close(); err != nil {
		return errors.New("couldn't save the sign-in")
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return errors.New("couldn't save the sign-in")
	}
	s.cache = maps.Clone(all)
	return nil
}

// get returns one sign-in.
func (s *tokenStore) get(key string) (tokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, _ := s.readLocked()
	e, ok := all[key]
	return e, ok && e.Access != ""
}

// put saves one sign-in.
func (s *tokenStore) put(key string, e tokenEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readLocked()
	if errors.Is(err, errNoTokenFolder) {
		return err
	}
	e.Saved = time.Now().UTC()
	all[key] = e
	return s.writeLocked(all)
}

// update changes one saved sign-in in place (refresh rotation); false
// when it's gone (signed out meanwhile).
func (s *tokenStore) update(key string, fn func(*tokenEntry)) (tokenEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readLocked()
	if err != nil {
		return tokenEntry{}, false, err
	}
	e, ok := all[key]
	if !ok {
		return tokenEntry{}, false, nil
	}
	fn(&e)
	e.Saved = time.Now().UTC()
	all[key] = e
	return e, true, s.writeLocked(all)
}

// remove forgets one sign-in (Sign out, or a refresh the platform refused).
func (s *tokenStore) remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readLocked()
	if err != nil {
		return err
	}
	if _, ok := all[key]; !ok {
		return nil
	}
	delete(all, key)
	return s.writeLocked(all)
}
