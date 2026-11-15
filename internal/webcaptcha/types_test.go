package webcaptcha

import (
	"encoding/json"
	"testing"
	"time"
)

func testImage() Image {
	return Image{
		Background: "background",
		Tile:       "tile",
		Width:      300,
		Height:     220,
		TileWidth:  64,
		TileHeight: 64,
		InitialX:   10,
		InitialY:   30,
	}
}

func TestImageJSON(t *testing.T) {
	raw, err := json.Marshal(testImage())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"background", "tile", "width", "height", "tile_width", "tile_height", "initial_x", "initial_y"}
	if len(fields) != len(want) {
		t.Fatalf("unexpected image fields: %s", raw)
	}
	for _, name := range want {
		if _, exists := fields[name]; !exists {
			t.Errorf("missing field %q", name)
		}
	}
	var got Image
	if err := json.Unmarshal(raw, &got); err != nil || got != testImage() {
		t.Fatalf("image round trip: %+v, %v", got, err)
	}
}

func TestPuzzleJSONOmitsAnswer(t *testing.T) {
	raw, err := json.Marshal(Puzzle{Image: testImage(), X: 150, Y: 30})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields["image"] == nil {
		t.Fatalf("puzzle exposes more than its image: %s", raw)
	}
}

func TestChallengeAndTicketJSON(t *testing.T) {
	expires := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		value  any
		fields []string
	}{
		{"challenge", Challenge{ChallengeID: "challenge", Image: testImage(), ExpiresAt: expires},
			[]string{"challenge_id", "image", "expires_at"}},
		{"ticket", Ticket{Ticket: "ticket", ExpiresAt: expires},
			[]string{"ticket", "expires_at"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.fields) {
				t.Fatalf("unexpected fields: %s", raw)
			}
			for _, name := range tc.fields {
				if _, exists := got[name]; !exists {
					t.Errorf("missing field %q", name)
				}
			}
			var decoded time.Time
			if err := json.Unmarshal(got["expires_at"], &decoded); err != nil || !decoded.Equal(expires) {
				t.Fatalf("expiry round trip: %v, %v", decoded, err)
			}
		})
	}
}
