//go:build sodium && cgo

package nativewire

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pierrec/lz4/v4"
	"google.golang.org/protobuf/encoding/protowire"
)

func msgNum(n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, n, protowire.VarintType), v)
}
func msgBytes(n protowire.Number, b []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), b)
}
func msgSource(n protowire.Number, uid uint64, name string) []byte {
	return msgBytes(n, append(msgNum(1, uid), msgBytes(2, []byte(name))...))
}
func fixturePlain() []byte {
	b := append(msgSource(1, 123, "synthetic-sender"), msgSource(2, 456, "synthetic-group")...)
	b = append(b, msgNum(3, 1700000000000)...)
	b = append(b, msgNum(4, 1)...)
	b = append(b, msgNum(5, 2)...)
	b = append(b, msgBytes(100, msgBytes(1, []byte("hello <script>not-html</script>")))...)
	b = append(b, msgBytes(301, append(msgBytes(1, []byte("synthetic-client")), msgBytes(100, msgBytes(1, []byte("quoted")))...))...)
	return b
}
func fixtureEnvelope(t testing.TB, key, plain []byte, compression bool, header uint64) string {
	t.Helper()
	aad := uint64(0)
	body := plain
	if compression {
		aad = 3
		block := make([]byte, lz4.CompressBlockBound(len(plain)))
		n, err := lz4.CompressBlock(plain, block, nil)
		if err != nil || n == 0 {
			t.Fatal("synthetic compression failed")
		}
		body = append(binary.LittleEndian.AppendUint32(nil, uint32(len(plain))), block[:n]...)
	}
	sealed, err := originalSeal(key, binary.LittleEndian.AppendUint64(nil, 777), body, binary.LittleEndian.AppendUint64(nil, aad))
	if err != nil {
		t.Fatal(err)
	}
	b := protowire.AppendFixed64(protowire.AppendTag(nil, 1, protowire.Fixed64Type), header)
	b = append(b, msgNum(3, 777)...)
	b = append(b, msgNum(6, aad)...)
	b = append(b, msgBytes(4, sealed[:len(sealed)-16])...)
	b = append(b, msgBytes(7, sealed[len(sealed)-16:])...)
	return base64.RawURLEncoding.EncodeToString(b)
}
func TestOpenApplicationEnvelope(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	header := uint64(123<<34 | 456<<4 | 2<<2 | 1)
	for _, compression := range []bool{false, true} {
		envelope := fixtureEnvelope(t, key, fixturePlain(), compression, header)
		for _, wrapped := range []bool{false, true} {
			text := envelope
			if wrapped {
				b, _ := json.Marshal(map[string]string{"b": text})
				text = string(b)
			}
			out, err := OpenMessage(key, text)
			if err != nil || out.Sender.ID != 123 || out.Target.ID != 456 ||
				out.Text != "hello <script>not-html</script>" || out.Quote == nil || out.Quote.Text != "quoted" {
				t.Fatal("bounded envelope fixture mismatch", err)
			}
		}
		// Native version-one packet signature layer, independent of app header.
		meta := msgNum(1, 999)
		signature, _ := originalSeal(key, binary.LittleEndian.AppendUint64(nil, 111), meta, binary.LittleEndian.AppendUint64(nil, 1))
		signature = append(append(binary.LittleEndian.AppendUint64(nil, 111), signature[len(signature)-16:]...), signature[:len(signature)-16]...)
		packet := append(msgBytes(1, []byte(envelope)), msgBytes(2, []byte(base64.RawURLEncoding.EncodeToString(signature)))...)
		packet = append(packet, msgNum(3, 1)...)
		if _, err := OpenMessage(key, base64.RawURLEncoding.EncodeToString(packet)); err != nil {
			t.Fatal("signed packet fixture", err)
		}
	}
	bad := fixtureEnvelope(t, key, fixturePlain(), true, header+1)
	_, err := OpenMessage(key, bad)
	var stage *DecodeError
	if !errors.As(err, &stage) || stage.Stage != "routing" {
		t.Fatal("routing header not bound")
	}
	bad = fixtureEnvelope(t, key, fixturePlain(), true, header)
	if _, err = OpenMessage(bytes.Repeat([]byte{18}, 32), bad); err == nil {
		t.Fatal("wrong authentication key accepted")
	}
	for _, raw := range []string{"", "%%", "Zg==", "Zh", "Zg\n", "Zg\r", `{"b":"abc","extra":1}`, `{"b":2}`, base64.RawURLEncoding.EncodeToString([]byte{0xff})} {
		if _, err = OpenMessage(key, raw); err == nil {
			t.Fatal("malformed message accepted")
		}
	}
}

func TestMessageFieldArenaPreservesRepeatedAndUniqueFields(t *testing.T) {
	data := append(msgBytes(2, []byte("first")), msgNum(3, 77)...)
	data = append(data, msgBytes(2, []byte("second"))...)
	data = append(data, msgNum(4, 88)...)
	fields, err := messageFields(data)
	if err != nil || len(fields[2]) != 2 || string(fields[2][0].b) != "first" ||
		string(fields[2][1].b) != "second" || fields[3][0].n != 77 || fields[4][0].n != 88 {
		t.Fatal("repeated field overwrote adjacent field", err)
	}
	if _, err := one(fields, 2, protowire.BytesType); err == nil {
		t.Fatal("duplicate singular field accepted")
	}
	data = bytes.Repeat(msgNum(1, 1), 1025)
	if _, err := messageFields(data); err == nil {
		t.Fatal("field limit lost")
	}
}

func TestMessageEncodedCanonicalBase64(t *testing.T) {
	for _, text := range []string{"Zg", `{"b":"Zg"}`} {
		decoded, err := messageEncoded(text)
		if err != nil || string(decoded) != "f" {
			t.Fatal("canonical encoding rejected", err)
		}
	}
	for _, text := range []string{"Zg==", "Zh", "Zg\n", "Z\rg", `{"b":"Zg\n"}`, " Zg"} {
		if _, err := messageEncoded(text); err == nil {
			t.Fatal("noncanonical encoding accepted")
		}
	}
}

func TestDesktopMessageTimestamp(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	plain := append(msgSource(1, 123, "synthetic"), msgSource(2, 456, "group")...)
	plain = append(plain, msgBytes(3, append(msgNum(1, 1700000000), msgNum(2, 123000000)...))...)
	plain = append(plain, msgNum(4, 1)...)
	plain = append(plain, msgNum(5, 2)...)
	plain = append(plain, msgBytes(100, msgBytes(1, []byte("desktop fixture")))...)
	out, err := OpenMessage(key, fixtureEnvelope(t, key, plain, true, uint64(123<<34|456<<4|2<<2|1)))
	if err != nil || out.CreatedAt != 1700000000123 || out.Text != "desktop fixture" {
		t.Fatal("desktop Timestamp schema", err)
	}
}
func FuzzOpenMessage(f *testing.F) {
	f.Add("not-a-message")
	f.Add(`{"b":""}`)
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > Limit*2+1 {
			t.Skip()
		}
		_, _ = OpenMessage(bytes.Repeat([]byte{9}, 32), text)
	})
}
func BenchmarkOpenMessage(b *testing.B) {
	key := bytes.Repeat([]byte{17}, 32)
	text := fixtureEnvelope(b, key, fixturePlain(), true, uint64(123<<34|456<<4|2<<2|1))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := OpenMessage(key, text); err != nil {
				b.Fatal(err)
			}
		}
	})
}
