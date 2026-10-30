package tui

import (
	"context"
	"errors"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// Messages from the backend, sent to the program by the goroutines below or
// returned by commands.
type (
	connectedMsg struct {
		system  client.SystemInfo
		wrapper *client.WrapperStatus
		err     error
	}
	snapshotMsg struct{ list client.DownloadList }
	feedMsg     struct{ m domain.DownloadFeedMessage }
	connMsg     struct{ s client.ConnState }
	detailMsg   struct{ d client.DownloadDetail }
	eventMsg    struct{ ev domain.Event }
	followedMsg struct{ id string }
	lookupMsg   struct {
		links   []string
		lookups []lookupResult
	}
	searchMsg struct {
		term string
		res  client.SearchResult
		err  error
	}
	albumsMsg struct {
		artist client.SearchHit
		res    client.ArtistAlbums
		err    error
	}
	submitMsg struct {
		batch domain.BatchSubmitResponse
		err   error
	}
	printMsg struct{ text string }
	loginMsg struct {
		status, loginID string
		err             error
	}
)

type lookupResult struct {
	link string
	l    client.Lookup
	err  error
}

// bus delivers messages from goroutines to the running program.
type bus struct{ p *tea.Program }

func (b *bus) send(msg tea.Msg) {
	if b.p != nil {
		b.p.Send(msg)
	}
}

// connect reads what the header shows.
func connect(ctx context.Context, c *client.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		sys, err := c.System(ctx)
		if err != nil {
			return connectedMsg{err: err}
		}
		msg := connectedMsg{system: sys}
		if w, err := c.WrapperStatus(ctx); err == nil {
			msg.wrapper = &w
		}
		return msg
	}
}

// followFeed keeps the job list current for the life of ctx.
func followFeed(ctx context.Context, c *client.Client, b *bus) {
	c.FollowFeed(ctx, client.FeedHandlers{
		Snapshot: func(l client.DownloadList) { b.send(snapshotMsg{l}) },
		Message:  func(m domain.DownloadFeedMessage) { b.send(feedMsg{m}) },
		State:    func(s client.ConnState) { b.send(connMsg{s}) },
	})
}

// followJob streams one job's items until it is finished.
func followJob(ctx context.Context, c *client.Client, b *bus, id string) {
	_ = c.FollowJob(ctx, id, client.JobHandlers{ // the feed reports how the job ended
		Detail: func(d client.DownloadDetail) { b.send(detailMsg{d}) },
		Event:  func(ev domain.Event) { b.send(eventMsg{ev}) },
	})
	b.send(followedMsg{id})
}

func lookupLinks(ctx context.Context, c *client.Client, links []string) tea.Cmd {
	return func() tea.Msg {
		out := make([]lookupResult, len(links))
		for i, l := range links {
			lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			res, err := c.Lookup(lctx, l)
			cancel()
			out[i] = lookupResult{link: l, l: res, err: err}
		}
		return lookupMsg{links: links, lookups: out}
	}
}

func search(ctx context.Context, c *client.Client, term string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		res, err := c.Search(ctx, client.SearchQuery{Term: term, Limit: 6})
		return searchMsg{term: term, res: res, err: err}
	}
}

func artistAlbums(ctx context.Context, c *client.Client, artist client.SearchHit) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		res, err := c.ArtistAlbums(ctx, artist.ID, client.PageQuery{Limit: 50})
		return albumsMsg{artist: artist, res: res, err: err}
	}
}

func submit(ctx context.Context, c *client.Client, urls []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		res, err := c.Submit(ctx, urls, nil)
		if err != nil && len(res.Results) > 0 {
			err = nil // the per-link results say why each was refused
		}
		return submitMsg{batch: res, err: err}
	}
}

func login(ctx context.Context, c *client.Client, username, password string) tea.Cmd {
	return func() tea.Msg {
		c := c.WithTimeout(client.LoginWait)
		var r struct {
			Status  string `json:"status"`
			LoginID string `json:"login_id"`
		}
		raw, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/login", nil, map[string]string{"username": username, "password": password})
		if err == nil {
			err = jsonInto(raw, &r)
		}
		return loginMsg{status: r.Status, loginID: r.LoginID, err: err}
	}
}

func twoStep(ctx context.Context, c *client.Client, loginID, code string) tea.Cmd {
	return func() tea.Msg {
		c := c.WithTimeout(client.LoginWait)
		var r struct {
			Status string `json:"status"`
		}
		raw, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/login/"+loginID+"/2fa", nil, map[string]string{"two_step_code": code})
		if err == nil {
			err = jsonInto(raw, &r)
		}
		return loginMsg{status: r.Status, err: err}
	}
}

// errText is what a person reads for an error: the API's message, or that the
// server did not answer.
func errText(err error) string {
	var api *client.APIError
	if errors.As(err, &api) && api.Message != "" {
		return api.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the server did not answer in time"
	}
	return err.Error()
}
