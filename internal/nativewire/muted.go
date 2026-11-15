// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"encoding/binary"
	"strconv"
)

// MutedMembersRequest is the reviewed SDK (8,27) read, not a group mutation.
func MutedMembersRequest(team string) ([]byte, error) {
	if !decimal(team, true) {
		return nil, ErrProtocol
	}
	id, err := strconv.ParseUint(team, 10, 64)
	if err != nil {
		return nil, ErrProtocol
	}
	return binary.LittleEndian.AppendUint64(nil, id), nil
}

// ParseMutedMembers returns only a complete, unambiguous individual mute list.
// An empty list does not establish the group's other speaking policies.
func ParseMutedMembers(body []byte, team string) (map[string]bool, error) {
	request, err := MutedMembersRequest(team)
	if err != nil || len(body) < 9 || len(body) > Limit ||
		binary.LittleEndian.Uint64(body[:8]) != binary.LittleEndian.Uint64(request) {
		return nil, ErrProtocol
	}
	count, offset, err := ReadVarint(body, 8, 32)
	if err != nil || count > 10000 {
		return nil, ErrProtocol
	}
	out := make(map[string]bool, count)
	for range count {
		fields, end, err := ReadProperties(body, offset)
		if err != nil {
			return nil, ErrProtocol
		}
		peer := string(fields[3])
		id, parse := strconv.ParseUint(peer, 10, 32)
		if parse != nil || id == 0 || !decimal(peer, true) || out[peer] ||
			string(fields[9]) != "1" || string(fields[13]) != "1" {
			return nil, ErrProtocol
		}
		if group, exists := fields[1]; exists && string(group) != team {
			return nil, ErrProtocol
		}
		out[peer], offset = true, end
	}
	if offset != len(body) {
		return nil, ErrProtocol
	}
	return out, nil
}
