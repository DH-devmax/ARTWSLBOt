package nativeauth

import (
	"context"
	"net/http"
	"testing"

	"wangshangliao/internal/nativewire"
)

func TestOversizedImageMetadataCannotStartDownload(t *testing.T) {
	// A nil transport client proves rejection happens before networking.
	for _, expected := range []*nativewire.MessageImage{
		{Asset: "123", Size: nativewire.MaxImageBytes + 1},
		{Asset: "123", Width: 4284, Height: 5712},
	} {
		_, err := FetchImage(context.Background(), (*http.Client)(nil), Deployment{}, "123", false, expected)
		if e, ok := err.(*Error); !ok || e.Code != "native_image_preview_limited" {
			t.Fatal("oversized preview was not rejected before download", err)
		}
	}
}
