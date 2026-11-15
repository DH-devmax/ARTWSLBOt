package nativewire

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PrivateTarget keeps the business identity distinct from its transport route.
// It is server evidence, never an authorization grant.
type PrivateTarget struct {
	ID        uint64
	Transport string
}

func ValidPrivateTransport(peer string) bool {
	return len(peer) > 0 && len(peer) <= 128 && utf8.ValidString(peer) &&
		strings.IndexFunc(peer, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

func PrivateTargetFor(self uint64, ownTransport string, message ApplicationMessage, from, to string) (PrivateTarget, error) {
	var out PrivateTarget
	if self == 0 || self >= 1<<30 || message.Session != 1 ||
		!ValidPrivateTransport(ownTransport) || !ValidPrivateTransport(from) || !ValidPrivateTransport(to) {
		return out, ErrProtocol
	}
	switch {
	case message.Sender.ID == self && message.Target.ID != self && from == ownTransport && to != ownTransport:
		out = PrivateTarget{ID: message.Target.ID, Transport: to}
	case message.Target.ID == self && message.Sender.ID != self && to == ownTransport && from != ownTransport:
		out = PrivateTarget{ID: message.Sender.ID, Transport: from}
	default:
		return out, ErrProtocol
	}
	if out.ID == 0 || out.ID >= 1<<30 {
		return PrivateTarget{}, ErrProtocol
	}
	return out, nil
}

func PrivateConversationKey(peer PrivateTarget) string {
	sum := sha256.Sum256([]byte("private\x00" + strconv.FormatUint(peer.ID, 10) + "\x00" + peer.Transport))
	return hex.EncodeToString(sum[:])
}
