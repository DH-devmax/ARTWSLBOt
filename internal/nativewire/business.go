// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"encoding/binary"

	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/xxh3"
)

const DefaultProtocolVersion uint64 = 0x019c411fdeaf

func Binding(requestID uint64, plain []byte, device, version uint64) (uint64, []byte) {
	var digest uint64
	if len(plain) > 0 {
		digest = xxh3.Hash(plain)
	}
	return digest, BindingDigest(requestID, digest, device, version)
}

// The Android 3.7.4 SDK supplies a Murmur3 body digest to the native
// exchange codec; the binding itself still uses XXH3-128.
func BindingDigest(requestID, digest, device, version uint64) []byte {
	input := make([]byte, 32)
	binary.LittleEndian.PutUint64(input, requestID)
	binary.LittleEndian.PutUint64(input[8:], digest)
	binary.LittleEndian.PutUint64(input[16:], device)
	binary.LittleEndian.PutUint64(input[24:], version)
	hash := xxh3.Hash128(input)
	aad := make([]byte, 16)
	binary.LittleEndian.PutUint64(aad, hash.Lo)
	binary.LittleEndian.PutUint64(aad[8:], hash.Hi)
	return aad
}

func SealBusiness(key []byte, requestID uint64, plain []byte, device, version uint64) ([]byte, error) {
	digest, _ := Binding(requestID, plain, device, version)
	return SealBusinessDigest(key, requestID, plain, digest, device, version)
}

func SealBusinessDigest(key []byte, requestID uint64, plain []byte, digest, device, version uint64) ([]byte, error) {
	if len(key) != 32 || len(plain) > Limit {
		return nil, ErrProtocol
	}
	compressed := make([]byte, 9, 9+len(plain)+32)
	copy(compressed, []byte{0x28, 0xb5, 0x2f, 0xfd, 0xa0})
	binary.LittleEndian.PutUint32(compressed[5:], uint32(len(plain)))
	if len(plain) == 0 {
		compressed = append(compressed, 1, 0, 0)
	}
	for start := 0; start < len(plain); start += 131072 {
		end := min(start+131072, len(plain))
		header := uint32(end-start) << 3
		if end == len(plain) {
			header |= 1
		}
		compressed = append(compressed, byte(header), byte(header>>8), byte(header>>16))
		compressed = append(compressed, plain[start:end]...)
	}
	materialInput := make([]byte, 8, len(compressed)+8)
	binary.LittleEndian.PutUint64(materialInput, requestID)
	materialInput = append(materialInput, compressed...)
	hash := xxh3.Hash128(materialInput)
	material := make([]byte, 16)
	binary.LittleEndian.PutUint64(material, hash.Lo)
	binary.LittleEndian.PutUint64(material[8:], hash.Hi)
	aad := BindingDigest(requestID, digest, device, version)
	sealed, err := originalSeal(key, material[:8], compressed, aad)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), sealed[len(sealed)-16:]...)
	out = append(out, material...)
	out = append(out, sealed[:len(sealed)-16]...)
	return out, nil
}

func OpenBusiness(key, aad, body []byte, limit int) ([]byte, error) {
	if len(key) != 32 || len(aad) != 16 || limit < 1 || limit > 3*Limit || len(body) < 33 || len(body) > limit+4096 {
		return nil, ErrProtocol
	}
	sealed := append([]byte(nil), body[32:]...)
	sealed = append(sealed, body[:16]...)
	compressed, err := originalOpen(key, body[16:24], sealed, aad)
	if err != nil {
		return nil, err
	}
	if err = oneZstdFrame(compressed); err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(limit)+4096), zstd.WithDecoderMaxWindow(uint64(max(limit, 1024))))
	if err != nil {
		return nil, ErrProtocol
	}
	defer decoder.Close()
	plain, err := decoder.DecodeAll(compressed, nil)
	if err != nil || len(plain) > limit {
		return nil, ErrProtocol
	}
	return plain, nil
}

// Reject concatenated/skippable frames and trailing bytes before bounded decoding.
func oneZstdFrame(data []byte) error {
	if len(data) < 6 || binary.LittleEndian.Uint32(data) != 0xfd2fb528 {
		return ErrProtocol
	}
	descriptor := data[4]
	if descriptor&0x18 != 0 {
		return ErrProtocol
	}
	single := descriptor&0x20 != 0
	offset := 5
	if !single {
		offset++
	}
	offset += []int{0, 1, 2, 4}[descriptor&3]
	fcs := []int{0, 2, 4, 8}[descriptor>>6]
	if single && descriptor>>6 == 0 {
		fcs = 1
	}
	offset += fcs
	if offset > len(data) {
		return ErrProtocol
	}
	for {
		if len(data)-offset < 3 {
			return ErrProtocol
		}
		header := uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16
		offset += 3
		kind := (header >> 1) & 3
		size := int(header >> 3)
		if kind == 3 || size > 131072 {
			return ErrProtocol
		}
		if kind == 1 {
			size = 1
		}
		if size > len(data)-offset {
			return ErrProtocol
		}
		offset += size
		if header&1 != 0 {
			break
		}
	}
	if descriptor&4 != 0 {
		offset += 4
	}
	if offset != len(data) {
		return ErrProtocol
	}
	return nil
}
