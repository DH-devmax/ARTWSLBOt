//go:build sodium

package nativeauth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDesktopDeviceFingerprintIsStable(t *testing.T) {
	d := syntheticDeployment()
	first, again := DescribeDevice(d), DescribeDevice(d)
	if first != again || first.BusinessClient != "desktop" || first.IMClient != "web" ||
		first.Coexistence != "unverified" || len(first.BusinessID) != 16 {
		t.Fatal("device identity is unstable or promises unverified desktop coexistence")
	}
	d.DeviceID = "different-synthetic-device"
	changed := DescribeDevice(d)
	if changed.IMID == first.IMID || changed.BusinessID != first.BusinessID {
		t.Fatal("business and IM device identities were conflated")
	}
	raw, _ := json.Marshal(changed)
	if strings.Contains(string(raw), d.SigningSeed) || strings.Contains(string(raw), d.DeviceID) {
		t.Fatal("raw native identity or key exposed")
	}
	d.ProtocolVersion = "3.7.4"
	removed := DescribeDevice(d)
	if removed.BusinessClient != "unsupported" || removed.BusinessID != "" || removed.IMID != "" {
		t.Fatal("removed mobile adapter was exposed as a usable device")
	}
}
