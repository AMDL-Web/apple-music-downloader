package media

import (
	"context"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/wrapper"
)

func TestValidateRequestAcceptsArtistURL(t *testing.T) {
	downloader := &Downloader{
		cfg:     config.Default(),
		wrapper: fakeDownloaderWrapper{status: wrapper.Status{Ready: true, Status: true, Regions: []string{"cn"}}},
	}
	result, err := downloader.ValidateRequest(context.Background(), "https://music.apple.com/cn/artist/example/1495777901")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != string(applemusic.TypeArtist) || result.Storefront != "cn" {
		t.Fatalf("validation = %+v, want artist/cn", result)
	}
}
