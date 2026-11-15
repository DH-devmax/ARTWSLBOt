package gateway

import "wangshangliao/internal/nativeauth"

type BusinessSource struct {
	ChatSource
	Reference *nativeauth.MessageReference
}

// ReadBusinessSource uses the existing authenticated projection. A missing
// recall route does not manufacture a substitute from display identifiers.
func (m *ReceiverManager) ReadBusinessSource(platform, account, key string, event int64,
	projection, source []byte) (BusinessSource, error) {
	message, err := m.ReadChatSource(platform, account, key, event, projection, source)
	if err != nil {
		return BusinessSource{}, err
	}
	out := BusinessSource{ChatSource: message}
	candidate, err := m.readMessageCandidate(platform, account, message.Group, key, event, projection, source)
	if err == nil && candidate.Reference.Sender == message.Sender {
		ref := candidate.Reference
		out.Reference = &ref
	}
	return out, nil
}
