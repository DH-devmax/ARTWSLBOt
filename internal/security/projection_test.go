package security

import "testing"

func TestDecodedMessageAADKeepsHistoricalNamespaceAndScope(t *testing.T) {
	if DecodedMessageAAD("account-a", 42) != "native-decoded:account-a:42" ||
		DecodedMessageAAD("account-a", 42) == DecodedMessageAAD("account-b", 42) ||
		DecodedMessageAAD("account-a", 42) == DecodedMessageAAD("account-a", 43) {
		t.Fatal("projection sealing namespace changed or lost scope")
	}
}
