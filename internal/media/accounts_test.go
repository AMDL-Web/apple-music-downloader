package media

import (
	"context"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/usertoken"
)

// A job reads with the token of the account that serves its storefront, and
// a record pinned to a storefront runs in it; one without, in the default
// account's region.
func TestTokenAndStorefrontFollowTheAccount(t *testing.T) {
	tokens := usertoken.NewReading(func(id string) (string, error) { return strings.Repeat(strings.ToUpper(id[:1]), 100), nil })
	tokens.SetAccounts([]usertoken.Account{{ID: "us", Region: "us", Active: true}, {ID: "jp", Region: "jp", Position: 1, Active: true}})
	var gotToken string
	d := newTestDownloader(&Downloader{
		cfg:    config.Default(),
		tokens: tokens,
		catalog: fakeDownloaderCatalog{
			station:      applemusic.Collection{ID: "ra.1", Type: applemusic.TypeStation, Name: "Station"},
			stationToken: &gotToken,
		},
	})
	d.storefront = "jp"
	if _, err := d.resolveCollection(context.Background(), applemusic.ParsedURL{Storefront: "jp", Type: applemusic.TypeStation, ID: "ra.1"}, true); err != nil {
		t.Fatal(err)
	}
	if gotToken != strings.Repeat("J", 100) {
		t.Fatalf("a jp job read with %q…, want the jp account's token", gotToken[:1])
	}
	ctx := context.Background()
	if sf, err := d.RunStorefront(ctx, "jp"); err != nil || sf != "jp" {
		t.Fatalf("pinned = %q, %v", sf, err)
	}
	if sf, err := d.RunStorefront(ctx, ""); err != nil || sf != "us" {
		t.Fatalf("unpinned = %q, %v, want the default account's region", sf, err)
	}
	if sf, which, ok := d.requestStorefront(ctx, applemusic.TypeAlbum, "JP"); !ok || sf != "jp" || which != "the link's storefront" {
		t.Fatalf("served link = %q %q %v", sf, which, ok)
	}
	if sf, _, ok := d.requestStorefront(ctx, applemusic.TypeAlbum, "kr"); !ok || sf != "us" {
		t.Fatalf("unserved link = %q %v", sf, ok)
	}
}
