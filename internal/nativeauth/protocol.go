// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"github.com/spaolacci/murmur3"
	"github.com/zeebo/xxh3"

	"wangshangliao/internal/nativewire"
)

const DesktopProtocolVersion = "2.8.3"
const MobileProtocolVersion = "3.7.4"

// Identity of the fixed, signed Android 3.7.4 APK, not a caller-selected hash.
const android374AppHash = "3C:F2:35:2F:6D:B2:85:67:98:25:B1:2F:06:5E:FC:5C:7A:AC:0F:6D:74:67:DA:4E:F9:50:05:58:C4:42:F1:96=5D499D6530B3C4DC2787F668CF85F7A676CB45B85E71F3FD8105006D1B732A45"

// MobileAppHash is the pinned APK identity required by mobile deployment imports.
const MobileAppHash = android374AppHash

const mobileExchangeVersion uint64 = 1703240058617

func supportedProtocol(version string) bool {
	return version == DesktopProtocolVersion
}

func supportedDesktop(platform string) bool {
	return platform == "" || platform == "windows" || platform == "macos"
}

func desktopClient(platform string) string {
	if platform == "macos" {
		return "10"
	}
	return "11"
}

func (c *Client) wireVersion() uint64 {
	if c.mobile {
		return mobileExchangeVersion
	}
	return nativewire.DefaultProtocolVersion
}

func (c *Client) bodyDigest(plain []byte) uint64 {
	if c.mobile {
		return murmur3.Sum64(plain)
	}
	if len(plain) == 0 {
		return 0
	}
	return xxh3.Hash(plain)
}

func (c *Client) deviceIdentity(public []byte) uint64 {
	if c.mobile {
		return javaAbs64(murmur3.Sum64(public))
	}
	return xxh3.Hash(public)
}

func javaAbs64(value uint64) uint64 {
	// Java Math.abs(long) preserves MIN_VALUE on overflow.
	if int64(value) < 0 {
		return -value
	}
	return value
}
