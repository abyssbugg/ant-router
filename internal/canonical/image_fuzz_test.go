package canonical

import "testing"

func FuzzRemoteImageURL(f *testing.F) {
	f.Add("https://example.com/image.png", "image/png")
	f.Add("http://example.com/image.png", "image/png")
	f.Add("/tmp/image.png", "image/jpeg")
	f.Fuzz(func(t *testing.T, rawURL, mime string) {
		image, err := NewRemoteImage(rawURL, mime, ImageDetailAuto, 0, 0)
		if err != nil {
			return
		}
		url, ok := image.URL()
		if !ok || url == "" || !image.Valid() || image.SourceType() != ImageSourceURL {
			t.Fatalf("accepted inconsistent remote image")
		}
	})
}
