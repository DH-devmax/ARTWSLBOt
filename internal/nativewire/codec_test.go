package nativewire

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestMetadataCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/request_metadata_280.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Vectors []struct {
			Fields []uint64 `json:"fields"`
			Wire   string   `json:"wire_hex"`
		} `json:"vectors"`
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Vectors) == 0 {
		t.Fatal("empty corpus")
	}
	for _, vector := range corpus.Vectors {
		got, err := Metadata(vector.Fields)
		if err != nil || hex.EncodeToString(got) != vector.Wire {
			t.Fatalf("metadata mismatch: %x %v", got, err)
		}
	}
}
func TestIndependentPacket(t *testing.T) {
	got, err := Packet(4, 10, 4660, nil)
	if err != nil || hex.EncodeToString(got) != "00040a341200" {
		t.Fatal(got, err)
	}
	frame, err := ReadPacket([]byte{172, 2, 4, 10, 52, 18, 2, 200, 0})
	if err != nil || frame.Code != 200 || frame.Serial != 4660 || frame.Service != 4 {
		t.Fatal(frame, err)
	}
}
func TestProperties(t *testing.T) {
	expected := "02130464656d6fe8070973796e746865746963"
	got, err := Properties([]Property{{19, []byte("demo")}, {1000, []byte("synthetic")}})
	if err != nil || hex.EncodeToString(got) != expected {
		t.Fatal(got, err)
	}
	for n := 0; n < len(got); n++ {
		if _, _, err := ReadProperties(got[:n], 0); err == nil {
			t.Fatal("truncated accepted", n)
		}
	}
	duplicate, _ := Properties([]Property{{1, []byte("a")}, {1, []byte("b")}})
	if _, _, err = ReadProperties(duplicate, 0); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestBinding(t *testing.T) {
	digest, aad := Binding(42, []byte(`{"synthetic":true}`), 99, DefaultProtocolVersion)
	if digest != 1991075964100698326 || hex.EncodeToString(aad) != "81a175cbdd38b3b2f20c00678eacef89" {
		t.Fatal(digest, hex.EncodeToString(aad))
	}
}
func TestSerialNeverWraps(t *testing.T) {
	var serial SerialAllocator
	for i := 1; i <= 60000; i++ {
		n, err := serial.Next()
		if err != nil || int(n) != i {
			t.Fatal(n, err)
		}
	}
	if _, err := serial.Next(); err == nil {
		t.Fatal("serial wrapped")
	}
}
func FuzzReadPacket(f *testing.F) {
	f.Add([]byte{0, 4, 10, 52, 18, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { ReadPacket(data); ReadProperties(data, 0); ReadVarint(data, 0, 64) })
}
