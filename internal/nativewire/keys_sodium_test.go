//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestSealedBoxCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/sealed_box_280.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Key     string `json:"synthetic_private_key_hex"`
		Vectors []struct {
			Plain string `json:"plaintext_hex"`
			Body  string `json:"ciphertext_hex"`
		} `json:"vectors"`
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString(corpus.Key)
	if len(corpus.Vectors) == 0 {
		t.Fatal("missing independent sealed-box vectors")
	}
	for _, vector := range corpus.Vectors {
		expected, _ := hex.DecodeString(vector.Plain)
		body, _ := hex.DecodeString(vector.Body)
		actual, err := OpenSealedBox(key, body)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("sealed box incompatible", err)
		}
		body[0] ^= 1
		if _, err := OpenSealedBox(key, body); err == nil {
			t.Fatal("modified sealed box accepted")
		}
	}
}

func TestSodiumSigningAndSipHash(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	public, private, err := SigningKeypair(seed)
	if err != nil {
		t.Fatal(err)
	}
	// A separate Go verifier checks the controlled libsodium implementation.
	expected := ed25519.NewKeyFromSeed(seed)
	if !bytes.Equal(private, expected) || !bytes.Equal(public, expected.Public().(ed25519.PublicKey)) {
		t.Fatal("Ed25519 seed mapping changed")
	}
	signature, err := Sign(private, []byte("synthetic-request"))
	if err != nil || !ed25519.Verify(public, []byte("synthetic-request"), signature) {
		t.Fatal("native signing incompatible")
	}
	vectors := []uint64{0x77cda97233d8f304, 0x80113eb1e3e30e2e, 0x4e032477ec15d740, 0x1d50511edbb70f6c, 0xbfbdd8c1b9b8b951, 0x1fea8e2c439acb3d, 0x5242244124713f97, 0xa16895fb40a26c3d}
	for variant, expected := range vectors {
		message, key := make([]byte, 24), make([]byte, 16)
		for i := range message {
			message[i] = byte(i*17 + variant*31)
		}
		for i := range key {
			key[i] = byte(i*13 + variant*29)
		}
		value, err := SipHash24(key, message)
		if err != nil || value != expected {
			t.Fatal("SipHash protocol vector incompatible", variant, err)
		}
	}
}
