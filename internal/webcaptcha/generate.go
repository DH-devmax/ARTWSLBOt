package webcaptcha

import (
	"sync"

	"github.com/wenlng/go-captcha-assets/resources/imagesv2"
	"github.com/wenlng/go-captcha-assets/resources/tiles"
	"github.com/wenlng/go-captcha/v2/slide"
)

// The implementation targets go-captcha/v2 v2.0.5 and go-captcha-assets v1.0.7.
var (
	generatorOnce sync.Once
	slider        slide.Captcha
	generatorErr  error
)

// Generate lazily decodes bundled resources and produces a basic slider puzzle.
// Library errors and panics fail closed without exposing their details.
func Generate() (out Puzzle, err error) {
	defer func() {
		if recover() != nil {
			out, err = Puzzle{}, ErrUnavailable
		}
	}()
	generatorOnce.Do(func() {
		// sync.Once also completes on panic; keep initialization failed then.
		generatorErr = ErrUnavailable
		backgrounds, err := imagesv2.GetImages()
		if err != nil {
			return
		}
		shapes, err := tiles.GetTiles()
		if err != nil {
			return
		}
		graphs := make([]*slide.GraphImage, 0, len(shapes))
		for _, shape := range shapes {
			graphs = append(graphs, &slide.GraphImage{
				OverlayImage: shape.OverlayImage,
				MaskImage:    shape.MaskImage,
				ShadowImage:  shape.ShadowImage,
			})
		}
		builder := slide.NewBuilder()
		builder.SetResources(slide.WithBackgrounds(backgrounds), slide.WithGraphImages(graphs))
		slider = builder.Make()
		generatorErr = nil
	})
	if generatorErr != nil || slider == nil {
		return Puzzle{}, ErrUnavailable
	}
	data, err := slider.Generate()
	if err != nil {
		return Puzzle{}, ErrUnavailable
	}
	background, err := data.GetMasterImage().ToBase64()
	if err != nil {
		return Puzzle{}, ErrUnavailable
	}
	tile, err := data.GetTileImage().ToBase64()
	if err != nil {
		return Puzzle{}, ErrUnavailable
	}
	block := data.GetData()
	size := slider.GetOptions().GetImageSize()
	return Puzzle{
		Image: Image{
			Background: background,
			Tile:       tile,
			Width:      size.Width,
			Height:     size.Height,
			TileWidth:  block.Width,
			TileHeight: block.Height,
			InitialX:   block.DX,
			InitialY:   block.DY,
		},
		X: block.X,
		Y: block.Y,
	}, nil
}
