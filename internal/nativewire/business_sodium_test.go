//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestOfficialAndroid374NativeExchangeVector(t *testing.T) {
	data, err := os.ReadFile("testdata/android374_exchange.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Plain, Key, Body string
		Digest           uint64
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]*string{
		"plain_hex": &vector.Plain, "key_hex": &vector.Key, "body_hex": &vector.Body,
	} {
		if err = json.Unmarshal(raw[name], target); err != nil {
			t.Fatal(err)
		}
	}
	if err = json.Unmarshal(raw["body_digest"], &vector.Digest); err != nil {
		t.Fatal(err)
	}
	plain, _ := hex.DecodeString(vector.Plain)
	key, _ := hex.DecodeString(vector.Key)
	expected, _ := hex.DecodeString(vector.Body)
	encoded, err := SealBusinessDigest(key, 42, plain, vector.Digest, 99, 1703240058617)
	if err != nil || !bytes.Equal(encoded, expected) {
		t.Fatalf("official native exchange mismatch: %x %v", encoded, err)
	}
	aad := BindingDigest(42, vector.Digest, 99, 1703240058617)
	if hex.EncodeToString(aad) != "bddbe6eadf23c22e3e4b7a8aee7cf691" {
		t.Fatal("official native AAD mismatch")
	}
	opened, err := OpenBusiness(key, aad, expected, Limit)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatal("official native frame did not decode", err)
	}
	// Old hash/version inputs must not authenticate a current frame.
	for _, wrong := range [][]byte{
		BindingDigest(42, vector.Digest, 99, 1703240058615),
		BindingDigest(42, vector.Digest+1, 99, 1703240058617),
	} {
		if _, err = OpenBusiness(key, wrong, expected, Limit); err == nil {
			t.Fatal("wrong request binding was accepted")
		}
	}
}

func TestIndependentBusinessVector(t *testing.T) {
	body, _ := hex.DecodeString("2cb4326bbc156bf266de89f03baf3d668bae2141b46cf95e39f54a7b9dfc9774f814ac191a984faca4160b4fbce0a4d3c199b112c0df126ede8ff0")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	expected := []byte(`{"synthetic":true}`)
	_, aad := Binding(42, expected, 99, DefaultProtocolVersion)
	plain, err := OpenBusiness(key, aad, body, Limit)
	if err != nil || !bytes.Equal(plain, expected) {
		t.Fatal(string(plain), err)
	}
	sealed, err := SealBusiness(key, 42, expected, 99, DefaultProtocolVersion)
	// The independent response above is compressed. The pinned request encoder
	// intentionally uses raw blocks; this separate value comes from protocol_oracle.py.
	request, _ := hex.DecodeString("318dca47bfd9bdb377fd04ab2b68391ef2ba2e78d82ef29427045744787f2e37fd6eeffb6ce72b58fc558543a50186064a6106160e119efd10a5eb9face8")
	if err != nil || !bytes.Equal(sealed, request) {
		t.Fatalf("encoder mismatch %x %v", sealed, err)
	}
	for _, i := range []int{0, 16, 32, len(body) - 1} {
		damaged := append([]byte(nil), body...)
		damaged[i] ^= 1
		if _, err := OpenBusiness(key, aad, damaged, Limit); err == nil {
			t.Fatal("tampering accepted")
		}
	}
}

func TestAuthenticatedCompressionBounds(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	for _, kind := range []string{"valid", "truncated", "trailing", "concatenated", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			plain := []byte("fixture")
			if kind == "oversize" {
				plain = bytes.Repeat([]byte("x"), Limit+1)
			}
			frame := encoder.EncodeAll(plain, nil)
			switch kind {
			case "truncated":
				frame = frame[:len(frame)-1]
			case "trailing":
				frame = append(frame, []byte("untrusted trailing bytes")...)
			case "concatenated":
				frame = append(frame, frame...)
			}
			sealed, err := originalSeal(make([]byte, 32), make([]byte, 8), frame, make([]byte, 16))
			if err != nil {
				t.Fatal(err)
			}
			envelope := append([]byte(nil), sealed[len(sealed)-16:]...)
			envelope = append(envelope, make([]byte, 16)...)
			envelope = append(envelope, sealed[:len(sealed)-16]...)
			result, err := OpenBusiness(make([]byte, 32), make([]byte, 16), envelope, Limit)
			if kind == "valid" {
				if err != nil || !bytes.Equal(result, plain) {
					t.Fatal("valid compressed response rejected", err)
				}
			} else if err == nil {
				t.Fatal("authenticated malformed or oversized compression accepted")
			}
		})
	}
}
func TestBusinessBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 255, 256, 131072, 131073, Limit} {
		expected := bytes.Repeat([]byte("x"), size)
		key := make([]byte, 32)
		body, err := SealBusiness(key, 42, expected, 99, DefaultProtocolVersion)
		if err != nil {
			t.Fatal(err)
		}
		_, aad := Binding(42, expected, 99, DefaultProtocolVersion)
		plain, err := OpenBusiness(key, aad, body, Limit)
		if err != nil || !bytes.Equal(expected, plain) {
			t.Fatal(size, err)
		}
	}
}
