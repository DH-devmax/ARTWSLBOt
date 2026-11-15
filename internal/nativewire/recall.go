package nativewire

import (
	"strconv"
	"unicode/utf8"
)

// RecallNotice is transport evidence only, never a role or permission proof.
type RecallNotice struct {
	ClientID   string `json:"client_id"`
	ServerID   string `json:"server_id,omitempty"`
	Actor      string `json:"actor"`
	Sender     string `json:"sender,omitempty"`
	Target     string `json:"-"`
	Scene      int    `json:"-"`
	RecalledAt uint64 `json:"recalled_at"`
}

// ReadRecallNotices reads the pinned SDK 7/14 and 7/15 sysMsg layouts outside
// ingestion. It neither produces ACKs nor interprets the roaming time as a cursor.
func ReadRecallNotices(frame Frame) ([]RecallNotice, error) {
	if frame.Code != 200 {
		return nil, nil
	}
	if frame.Service == 4 && (frame.Command == 1 || frame.Command == 2 || frame.Command == 10 || frame.Command == 11) {
		if len(frame.Body) < 8 {
			return nil, ErrProtocol
		}
		inner, err := ReadPacket(frame.Body[8:])
		if err != nil || inner.Service == 4 {
			return nil, ErrProtocol
		}
		return ReadRecallNotices(inner)
	}
	if frame.Service != 7 || frame.Command != 14 && frame.Command != 15 {
		return nil, nil
	}
	count, offset := uint64(1), 0
	var err error
	if frame.Command == 15 {
		count, offset, err = ReadVarint(frame.Body, 0, 32)
		if err != nil || count > 128 {
			return nil, ErrProtocol
		}
	}
	out := []RecallNotice{}
	for range count {
		fields, next, err := ReadProperties(frame.Body, offset)
		if err != nil {
			return nil, ErrProtocol
		}
		offset = next
		kind := string(fields[1])
		if kind != "7" && kind != "8" {
			continue
		}
		recall := RecallNotice{ClientID: string(fields[10]), ServerID: string(fields[11]),
			Actor: string(fields[16]), Sender: string(fields[3]), Target: string(fields[2])}
		if recall.Actor == "" {
			recall.Actor = recall.Sender
		}
		if kind == "8" {
			recall.Scene = 1
		}
		if !identifier(recall.ClientID) || !decimal(recall.ServerID, true) ||
			!identifier(recall.Actor) || !identifier(recall.Sender) || !identifier(recall.Target) ||
			!utf8.Valid(fields[0]) || !decimal(string(fields[0]), true) {
			return nil, ErrProtocol
		}
		recall.RecalledAt, _ = strconv.ParseUint(string(fields[0]), 10, 64)
		out = append(out, recall)
	}
	if frame.Command == 15 {
		if len(frame.Body)-offset != 9 || frame.Body[len(frame.Body)-1] != 1 && frame.Body[len(frame.Body)-1] != 2 {
			return nil, ErrProtocol
		}
		offset += 9
	}
	if offset != len(frame.Body) {
		return nil, ErrProtocol
	}
	return out, nil
}
