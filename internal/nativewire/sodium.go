//go:build sodium && cgo

package nativewire

/*
#cgo pkg-config: libsodium
#include <sodium.h>
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"unsafe"
)

func init() {
	if C.sodium_init() < 0 {
		panic("libsodium initialization failed")
	}
}

func pointer(data []byte) *C.uchar {
	if len(data) == 0 {
		return nil
	}
	return (*C.uchar)(unsafe.Pointer(&data[0]))
}

func originalSeal(key, nonce, plain, aad []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 8 {
		return nil, ErrProtocol
	}
	out := make([]byte, len(plain)+16)
	var size C.ulonglong
	code := C.crypto_aead_chacha20poly1305_encrypt(pointer(out), &size, pointer(plain), C.ulonglong(len(plain)),
		pointer(aad), C.ulonglong(len(aad)), nil, pointer(nonce), pointer(key))
	if code != 0 || int(size) != len(out) {
		return nil, errors.New("native_encryption_failed")
	}
	return out, nil
}

func originalOpen(key, nonce, sealed, aad []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 8 || len(sealed) < 16 {
		return nil, ErrProtocol
	}
	out := make([]byte, max(1, len(sealed)-16))
	var size C.ulonglong
	code := C.crypto_aead_chacha20poly1305_decrypt(pointer(out), &size, nil, pointer(sealed), C.ulonglong(len(sealed)),
		pointer(aad), C.ulonglong(len(aad)), pointer(nonce), pointer(key))
	if code != 0 {
		return nil, ErrProtocol
	}
	return out[:int(size)], nil
}

func OpenSealedBox(privateKey, ciphertext []byte) ([]byte, error) {
	if len(privateKey) != 32 || len(ciphertext) < 48 || len(ciphertext) > Limit+48 {
		return nil, ErrProtocol
	}
	public := make([]byte, 32)
	if C.crypto_scalarmult_base(pointer(public), pointer(privateKey)) != 0 {
		return nil, ErrProtocol
	}
	plain := make([]byte, max(1, len(ciphertext)-48))
	if C.crypto_box_seal_open(pointer(plain), pointer(ciphertext), C.ulonglong(len(ciphertext)), pointer(public), pointer(privateKey)) != 0 {
		return nil, ErrProtocol
	}
	return plain[:len(ciphertext)-48], nil
}

func Available() bool { return true }

func SealBox(publicKey, plaintext []byte) ([]byte, error) {
	if len(publicKey) != 32 || len(plaintext) > Limit {
		return nil, ErrProtocol
	}
	out := make([]byte, len(plaintext)+48)
	if C.crypto_box_seal(pointer(out), pointer(plaintext), C.ulonglong(len(plaintext)), pointer(publicKey)) != 0 {
		return nil, ErrProtocol
	}
	return out, nil
}

func CurvePrivate(signingPrivate []byte) ([]byte, error) {
	if len(signingPrivate) != 64 {
		return nil, ErrProtocol
	}
	out := make([]byte, 32)
	if C.crypto_sign_ed25519_sk_to_curve25519(pointer(out), pointer(signingPrivate)) != 0 {
		return nil, ErrProtocol
	}
	return out, nil
}

func SigningKeypair(seed []byte) (public, private []byte, err error) {
	if len(seed) != 32 {
		return nil, nil, ErrProtocol
	}
	public, private = make([]byte, 32), make([]byte, 64)
	if C.crypto_sign_seed_keypair(pointer(public), pointer(private), pointer(seed)) != 0 {
		return nil, nil, ErrProtocol
	}
	return public, private, nil
}

func Sign(private, message []byte) ([]byte, error) {
	if len(private) != 64 || len(message) > Limit {
		return nil, ErrProtocol
	}
	signature := make([]byte, 64)
	var size C.ulonglong
	if C.crypto_sign_detached(pointer(signature), &size, pointer(message), C.ulonglong(len(message)), pointer(private)) != 0 || size != 64 {
		return nil, ErrProtocol
	}
	return signature, nil
}

func SipHash24(key, message []byte) (uint64, error) {
	if len(key) != 16 || len(message) > Limit {
		return 0, ErrProtocol
	}
	out := make([]byte, 8)
	if C.crypto_shorthash_siphash24(pointer(out), pointer(message), C.ulonglong(len(message)), pointer(key)) != 0 {
		return 0, ErrProtocol
	}
	return binary.LittleEndian.Uint64(out), nil
}
