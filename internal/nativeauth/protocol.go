// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"github.com/spaolacci/murmur3"
	"github.com/zeebo/xxh3"

	"wangshangliao/internal/nativewire"
)

// Client identity is derived from the pinned, signed official APK, not from
// its version string alone. See docs/official-client-research.md.
const android374AppHash = "3C:F2:35:2F:6D:B2:85:67:98:25:B1:2F:06:5E:FC:5C:7A:AC:0F:6D:74:67:DA:4E:F9:50:05:58:C4:42:F1:96=5D499D6530B3C4DC2787F668CF85F7A676CB45B85E71F3FD8105006D1B732A45"

func supportedProtocol(version string) bool {
	switch version {
	case "3.7.1", "3.7.3", "3.7.4", "2.8.3":
		return true
	}
	return false
}

func (c *Client) android() bool {
	return c.config.ProtocolVersion == "3.7.3" || c.config.ProtocolVersion == "3.7.4"
}

func (c *Client) wireVersion() uint64 {
	switch c.config.ProtocolVersion {
	case "3.7.4":
		return 1703240058617
	case "3.7.3":
		return 1703240058615
	}
	return nativewire.DefaultProtocolVersion
}

func (c *Client) bodyDigest(plain []byte) uint64 {
	if c.config.ProtocolVersion == "3.7.4" {
		low, _ := murmur3.Sum128(plain)
		return low
	}
	if len(plain) == 0 {
		return 0
	}
	return xxh3.Hash(plain)
}

func (c *Client) deviceIdentity(public []byte) uint64 {
	if c.config.ProtocolVersion == "3.7.4" {
		low, _ := murmur3.Sum128(public)
		// Match Java Math.abs(long), including the MIN_VALUE overflow case.
		if int64(low) < 0 {
			return -low
		}
		return low
	}
	return xxh3.Hash(public)
}
