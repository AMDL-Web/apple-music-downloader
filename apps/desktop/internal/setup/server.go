package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"amdl/internal/buildinfo"
	"amdl/internal/client"
	"amdl/internal/format"
)

// Line is one finding of a test: its state and what it found.
type Line struct {
	State State
	Text  string
}

// Server is what a test found about a remote amdl, each part Unchecked when
// an earlier one failed. Anything not Ready there is fixed on that server.
type Server struct {
	Address string
	State   State
	// Reach is the server's answer and version; Account the Apple ID its
	// wrapper has signed in; Token its media-user-token, which is
	// optional; FFmpeg its ffmpeg; Folder its downloads folder.
	Reach, Account, Token, FFmpeg, Folder Line
	Taken                                 time.Duration
}

// serverTimeout bounds a whole test of a server.
const serverTimeout = 15 * time.Second

// lowSpace is the free space below which a downloads folder is reported as
// nearly full: about one lossless album in high resolution.
const lowSpace = 2 << 30

// TestServer tests the amdl at address off the UI thread.
func (s *Setup) TestServer(address string) {
	s.Server = Server{Address: address, State: Checking}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), serverTimeout)
		defer cancel()
		got := Diagnose(ctx, address, buildinfo.Get().Version)
		s.post(func() {
			if s.Server.Address == address && s.Server.State == Checking {
				s.Server = got
			}
		})
	}()
}

// Diagnose tests the amdl at address for a client of version mine.
func Diagnose(ctx context.Context, address, mine string) Server {
	sv := Server{Address: address, State: Failed}
	c, err := client.New(address)
	if err != nil {
		sv.Reach = Line{Failed, err.Error()}
		return sv
	}
	start := time.Now()
	if _, err := c.Health(ctx); err != nil {
		sv.Reach = Line{Failed, "No answer · " + short(err)}
		return sv
	}
	sys, err := c.System(ctx)
	sv.Taken = time.Since(start)
	if err != nil {
		sv.Reach = Line{Failed, "Not an amdl server · " + short(err)}
		return sv
	}
	sv.State = Ready
	sv.Reach = Line{Ready, fmt.Sprintf("amdl %s · %d ms", sys.Version, sv.Taken.Milliseconds())}
	if !Compatible(sys.Version, mine) {
		sv.Reach = Line{Failed, fmt.Sprintf("amdl %s · this app needs amdl %d", sys.Version, major(mine))}
	}
	sv.FFmpeg = Line{Failed, "Missing"}
	for _, t := range sys.Tools {
		if t.Name == "ffmpeg" && t.Available {
			sv.FFmpeg = Line{Ready, t.Path}
		}
	}
	sv.Folder = folderLine(sys.Storage)
	ws, err := c.WrapperStatus(ctx)
	switch {
	case err != nil:
		sv.Account = Line{Failed, "No wrapper · " + short(err)}
	case !ws.Ready:
		sv.Account = Line{Working, "Wrapper starting"}
	case !ws.LoggedIn:
		sv.Account = Line{Missing, "Not signed in"}
	default:
		sv.Account = Line{Ready, AccountText(ws.Accounts, ws.Regions)}
	}
	saved := savedToken(ctx, c)
	switch {
	case err == nil && ws.MediaUserToken != "":
		sv.Token = Line{Ready, "From the wrapper · ends …" + tail(ws.MediaUserToken)}
	case saved != "":
		sv.Token = Line{Ready, "Saved · ends …" + tail(saved)}
	default:
		sv.Token = Line{Missing, "None"}
	}
	return sv
}

// AccountText names the signed-in accounts and their storefronts, or says
// one is signed in when the wrapper names none.
func AccountText(accounts, regions []string) string {
	parts := []string{}
	if len(accounts) > 0 {
		parts = append(parts, strings.Join(accounts, ", "))
	}
	if len(regions) > 0 {
		parts = append(parts, strings.Join(regions, ", "))
	}
	if len(parts) == 0 {
		return "Signed in"
	}
	return strings.Join(parts, " · ")
}

func folderLine(storage []client.Storage) Line {
	for _, st := range storage {
		if st.Name != "downloads_dir" {
			continue
		}
		return FolderLine(st)
	}
	return Line{Unchecked, ""}
}

// FolderLine reports a downloads folder: its path and free space, nearly
// full below lowSpace.
func FolderLine(st client.Storage) Line {
	switch {
	case st.Error != "":
		return Line{Failed, st.Path + " · " + st.Error}
	case st.TotalBytes > 0 && st.FreeBytes < lowSpace:
		return Line{Missing, st.Path + " · " + format.Bytes(st.FreeBytes) + " free"}
	case st.TotalBytes > 0:
		return Line{Ready, st.Path + " · " + format.Bytes(st.FreeBytes) + " free"}
	}
	return Line{Ready, st.Path}
}

// savedToken is the server's saved media-user-token, masked, or "".
func savedToken(ctx context.Context, c *client.Client) string {
	raw, err := c.Call(ctx, http.MethodGet, "/api/v1/config", nil, nil)
	if err != nil {
		return ""
	}
	var v struct {
		Config struct {
			Catalog struct {
				Token string `json:"media_user_token"`
			} `json:"catalog"`
		} `json:"config"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Config.Catalog.Token
}

func tail(token string) string { return token[max(0, len(token)-4):] }

// Compatible reports whether a client of version mine can use a server of
// version theirs: the same major version, or either one a development build.
func Compatible(theirs, mine string) bool {
	a, b := major(theirs), major(mine)
	return a <= 0 || b <= 0 || a == b
}

// major is a release's major version, 0 for a development build.
func major(v string) int {
	v = strings.TrimPrefix(v, "v")
	n, err := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	if err != nil || strings.Contains(v, "-dev") {
		return 0
	}
	return n
}

func short(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s) > 60 {
		s = s[i+2:]
	}
	if len(s) > 60 {
		s = s[:57] + "…"
	}
	return s
}
