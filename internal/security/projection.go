package security

import "strconv"

// DecodedMessageAAD binds a sealed projection to its original account and event.
func DecodedMessageAAD(account string, event int64) string {
	return "native-decoded:" + account + ":" + strconv.FormatInt(event, 10)
}
