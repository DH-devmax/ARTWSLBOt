package webcaptcha

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"testing"
)

func TestGenerateImageStructure(t *testing.T) {
	for i := 0; i < 3; i++ {
		puzzle, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		checkGeneratedPuzzle(t, puzzle)
	}
}

func TestGenerateConcurrent(t *testing.T) {
	type result struct {
		puzzle Puzzle
		err    error
	}
	results := make(chan result, 2)
	for i := 0; i < cap(results); i++ {
		go func() {
			puzzle, err := Generate()
			results <- result{puzzle, err}
		}()
	}
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err != nil {
			t.Error(got.err)
			continue
		}
		checkGeneratedPuzzle(t, got.puzzle)
	}
}

func checkGeneratedPuzzle(t *testing.T, puzzle Puzzle) {
	t.Helper()
	img := puzzle.Image
	if img.Width != 300 || img.Height != 220 {
		t.Fatalf("background size: %dx%d", img.Width, img.Height)
	}
	if img.TileWidth < 60 || img.TileWidth > 70 || img.TileHeight != img.TileWidth {
		t.Fatalf("tile size: %dx%d", img.TileWidth, img.TileHeight)
	}
	checkEncodedImage(t, img.Background, "jpeg", img.Width, img.Height)
	checkEncodedImage(t, img.Tile, "png", img.TileWidth, img.TileHeight)
	for _, point := range []struct {
		name string
		x, y int
	}{
		{"initial", img.InitialX, img.InitialY},
		{"answer", puzzle.X, puzzle.Y},
	} {
		if point.x < 0 || point.y < 0 || point.x > img.Width-img.TileWidth || point.y > img.Height-img.TileHeight {
			t.Errorf("%s position outside image: (%d,%d)", point.name, point.x, point.y)
		}
	}
	if img.InitialY != puzzle.Y || img.InitialX >= puzzle.X {
		t.Errorf("basic slider positions: initial=(%d,%d), answer=(%d,%d)", img.InitialX, img.InitialY, puzzle.X, puzzle.Y)
	}
}

func checkEncodedImage(t *testing.T, encoded, format string, width, height int) {
	t.Helper()
	prefix := "data:image/" + format + ";base64,"
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatalf("expected %s image data URL", format)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		t.Fatal(err)
	}
	decoded, gotFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if gotFormat != format || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		t.Fatalf("decoded image: %s %v; want %s %dx%d", gotFormat, decoded.Bounds(), format, width, height)
	}
}
