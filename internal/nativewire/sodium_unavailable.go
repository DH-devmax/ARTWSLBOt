//go:build !sodium || !cgo

package nativewire

import "errors"

var ErrNativeCryptoUnavailable = errors.New("native_crypto_requires_sodium_build")

func originalSeal(_, _, _, _ []byte) ([]byte, error)  { return nil, ErrNativeCryptoUnavailable }
func originalOpen(_, _, _, _ []byte) ([]byte, error)  { return nil, ErrNativeCryptoUnavailable }
func OpenSealedBox(_, _ []byte) ([]byte, error)       { return nil, ErrNativeCryptoUnavailable }
func SigningKeypair(_ []byte) ([]byte, []byte, error) { return nil, nil, ErrNativeCryptoUnavailable }
func Sign(_, _ []byte) ([]byte, error)                { return nil, ErrNativeCryptoUnavailable }
func SipHash24(_, _ []byte) (uint64, error)           { return 0, ErrNativeCryptoUnavailable }
func Available() bool                                 { return false }
func SealBox(_, _ []byte) ([]byte, error)             { return nil, ErrNativeCryptoUnavailable }
func CurvePrivate(_ []byte) ([]byte, error)           { return nil, ErrNativeCryptoUnavailable }
