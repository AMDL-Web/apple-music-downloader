package jobs

import (
	"context"
	"strings"
	"testing"

	"amdl/internal/domain"
)

// storefronts serves the listed regions; the first is the default account's.
type storefronts []string

func (s storefronts) Serves(sf string) bool {
	for _, r := range s {
		if strings.EqualFold(r, sf) {
			return true
		}
	}
	return false
}

func (s storefronts) DefaultRegion() string { return s[0] }

// A link's record is pinned to its storefront when an account serves it, and
// its run's input names the storefront it is read in.
func TestSubmitPinsTheLinksServedStorefront(t *testing.T) {
	m := newTestManager(t)
	m.SetStorefronts(storefronts{"us", "jp"})
	ctx := context.Background()
	resp := m.SubmitBatch(ctx, []string{"album|jp|1", "album|kr|2", "song|JP|3", "artist|jp|4"}, nil)
	if resp.Accepted != 4 {
		t.Fatalf("submit = %+v", resp.Results)
	}
	jp, err := m.store.GetAlbum(ctx, "1")
	if err != nil || jp.PinnedStorefront != "jp" {
		t.Fatalf("jp album pin = %q, %v", jp.PinnedStorefront, err)
	}
	kr, err := m.store.GetAlbum(ctx, "2")
	if err != nil || kr.PinnedStorefront != "" {
		t.Fatalf("kr album pin = %q, %v, want none: no account serves kr", kr.PinnedStorefront, err)
	}
	pending, err := m.store.ListPending(ctx, "")
	if err != nil || len(pending) != 2 || pending[0].Storefront != "jp" || pending[1].Storefront != "jp" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if !strings.Contains(resp.Results[0].Job.Input, "/jp/album/1") || !strings.Contains(resp.Results[1].Job.Input, "/us/album/2") {
		t.Fatalf("inputs = %q, %q", resp.Results[0].Job.Input, resp.Results[1].Job.Input)
	}
	if pin, err := m.RecordPin(ctx, domain.ResourceAlbum, "1"); err != nil || pin != "jp" {
		t.Fatalf("RecordPin = %q, %v", pin, err)
	}
}

// A pending add is read in its own storefront, and its records take it as
// their pin; one without reads in the default account's.
func TestResolverReadsInThePendingStorefront(t *testing.T) {
	m := newTestManager(t)
	catalog := &storefrontCatalog{albums: map[string]string{"jp/10": "100", "cn/11": "110"}}
	m.SetCatalog(catalog)
	ctx := context.Background()
	for _, p := range []domain.PendingAdd{
		{Kind: domain.PendingSong, CatalogID: "10", Source: domain.SourceManual, Storefront: "jp"},
		{Kind: domain.PendingSong, CatalogID: "11", Source: domain.SourceManual},
	} {
		if _, err := m.store.AddPending(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	m.resolvePending(ctx)
	if a, err := m.store.GetAlbum(ctx, "100"); err != nil || a.PinnedStorefront != "jp" {
		t.Fatalf("album 100 = %q, %v", a.PinnedStorefront, err)
	}
	if a, err := m.store.GetAlbum(ctx, "110"); err != nil || a.PinnedStorefront != "" {
		t.Fatalf("album 110 = %q, %v", a.PinnedStorefront, err)
	}
}

type storefrontCatalog struct{ albums map[string]string }

func (c *storefrontCatalog) AccountStorefront(context.Context) (string, error) { return "cn", nil }

func (c *storefrontCatalog) SongAlbumID(_ context.Context, storefront, songID string) (string, error) {
	if id, ok := c.albums[storefront+"/"+songID]; ok {
		return id, nil
	}
	return "", nonRetryableErr{"not in this storefront"}
}

func (c *storefrontCatalog) ArtistAlbumIDs(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}
