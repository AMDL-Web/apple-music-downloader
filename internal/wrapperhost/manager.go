package wrapperhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"amdl/internal/wrapper"
)

// Instance is an account reported by the manager's /status.instances.
// ID is the manager's UUIDv5 and is also the token selector and logout id.
type Instance struct {
	ID       string `json:"id"`
	Region   string `json:"region"`
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Degraded bool   `json:"degraded"`
}

// Instances discovers local accounts through HTTP, including degraded ones.
func (h Home) Instances(ctx context.Context) ([]Instance, error) {
	in, err := h.Current()
	if err != nil {
		return nil, err
	}
	var data struct {
		Instances []Instance `json:"instances"`
	}
	if err := managerCall(ctx, in.URL(), http.MethodGet, "/status", nil, &data); err != nil {
		return nil, err
	}
	if data.Instances == nil {
		data.Instances = []Instance{}
	}
	sort.Slice(data.Instances, func(i, j int) bool { return data.Instances[i].ID < data.Instances[j].ID })
	return data.Instances, nil
}

// InstanceToken reads one account's token. No token is read from the disk.
func (h Home) InstanceToken(ctx context.Context, id string) (wrapper.TokenReply, error) {
	if id == "" {
		return wrapper.TokenReply{}, wrapper.ErrNoToken
	}
	in, err := h.Current()
	if err != nil {
		return wrapper.TokenReply{}, err
	}
	var token wrapper.TokenReply
	err = managerCall(ctx, in.URL(), http.MethodGet, "/token?"+url.Values{"instance": {id}}.Encode(), nil, &token)
	return token, err
}

// managerCall never includes response bodies or decoder errors in errors:
// even a malformed error response can contain a token or password.
func managerCall(ctx context.Context, base, method, endpoint string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return errors.New("encode manager request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("build manager request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("manager request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return wrapper.ErrNoToken
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manager HTTP %d", resp.StatusCode)
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return errors.New("read manager reply")
	}
	if len(raw) > 64<<10 || json.Unmarshal(raw, &env) != nil {
		return errors.New("invalid manager reply")
	}
	if env.Code != 0 {
		return fmt.Errorf("manager reply code %d", env.Code)
	}
	if out != nil && json.Unmarshal(env.Data, out) != nil {
		return errors.New("invalid manager data")
	}
	return nil
}
