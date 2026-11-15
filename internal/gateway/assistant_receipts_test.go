package gateway

import "testing"

func TestAssistantReceiptRequiresNativeIdentityNotMatchingText(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		d := DecodedMessage{Platform: platform, NativeTarget: "123", NativeSender: "456", ServerID: "native-server"}
		d.Message.Session = 2
		d.Message.Target.ID = 123
		d.Message.Sender.ID = 456
		d.Message.ClientID = "stable-client"
		d.Message.Text = "same text"
		if !matchingAssistantReceipt(d, platform, "123", "stable-client", "456") {
			t.Fatal("valid identifier rejected", platform)
		}
		for _, args := range [][3]string{{"other-group", "stable-client", "456"}, {"123", "another-client", "456"}, {"123", "stable-client", "other-sender"}, {"123", "", "456"}} {
			if matchingAssistantReceipt(d, platform, args[0], args[1], args[2]) {
				t.Fatal("scope or text-only match accepted", platform, args)
			}
		}
		d.ServerID = ""
		if matchingAssistantReceipt(d, platform, "123", "stable-client", "456") {
			t.Fatal("missing native receipt ID")
		}
	}
}
