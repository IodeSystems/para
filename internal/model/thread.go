package model

import (
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/paramd"
)

// ThreadKind says what a thread key names.
type ThreadKind string

const (
	ThreadItem    ThreadKind = "item"    // an item id; the item's own thread
	ThreadTag     ThreadKind = "tag"     // "#tag", only when named in threads
	ThreadChannel ThreadKind = "channel" // "channel:<source-id>/<thread-id>"
)

// RequestTurnKind is what an owner turn on a request asks for.
type RequestTurnKind string

const (
	RequestTurnModify   RequestTurnKind = "modify"
	RequestTurnAsk      RequestTurnKind = "ask"
	RequestTurnRedirect RequestTurnKind = "redirect"
	RequestTurnOther    RequestTurnKind = "other"
)

// Enum lists the allowed values, for schema generation.
func (RequestTurnKind) Enum() []string { return enumStrings(RequestTurnKinds) }

// RequestTurnKinds lists every RequestTurnKind.
var RequestTurnKinds = []RequestTurnKind{RequestTurnModify, RequestTurnAsk, RequestTurnRedirect, RequestTurnOther}

// ParseThreadKey splits a thread key. The value is the item id, the tag without
// "#", or "<source-id>/<thread-id>". The key must be canonical: use
// NormalizeThreadKey first for owner input.
func ParseThreadKey(s string) (ThreadKind, string, error) {
	k, v, err := paramd.ParseThreadKey(s)
	if err != nil {
		return "", "", fmt.Errorf("model: parse thread key: %w", err)
	}
	return ThreadKind(k), v, nil
}

// NormalizeThreadKey trims a key and canonicalizes a tag the way tags are
// stored: lower case, spaces as '-', one leading '#'. It returns the parse error
// when the result is not a key.
func NormalizeThreadKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		s = "#" + strings.Join(strings.Fields(strings.ToLower(strings.TrimPrefix(s, "#"))), "-")
	}
	if _, _, err := ParseThreadKey(s); err != nil {
		return "", fmt.Errorf("model: normalize thread key: %w", err)
	}
	return s, nil
}

// ThreadKeyItem is the key of an item's own thread: its id. It is never stored.
func ThreadKeyItem(id string) string { return id }

// ThreadKeyTag is the key that joins a tag thread.
func ThreadKeyTag(tag string) string { return "#" + tag }

// ThreadKeyChannel is the key of a connector conversation.
func ThreadKeyChannel(source, thread string) string { return "channel:" + source + "/" + thread }

// ThreadKey is the key io answers: the item named by io.thread. Empty when
// the io answers nothing.
func (m *IO) ThreadKey() string { return m.Thread.Target() }

// ThreadKey is the channel key of the conversation the message is in. It is
// computed from via, never stored. Empty when via names no thread.
func (v IOVia) ThreadKey() string {
	if v.Source == "" || v.Thread == "" {
		return ""
	}
	return ThreadKeyChannel(v.Source, v.Thread)
}
