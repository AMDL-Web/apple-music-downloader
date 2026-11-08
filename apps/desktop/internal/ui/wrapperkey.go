package ui

import (
	"github.com/egoist/mygo/ui"

	"amdl/internal/wrapper"
)

// wrapperKeyField is the wrapper.token field when the backend has the key
// and its wrapper is not the guest this computer runs; nil otherwise.
func (a *App) wrapperKeyField() *configField {
	s := a.Store
	if w := s.Server.Wrapper; w != nil && w.Managed != nil {
		return nil
	}
	if _, ok := s.Config.Value("wrapper.token").(string); !ok {
		return nil
	}
	for i := range configFields {
		if configFields[i].key == "wrapper.token" {
			return &configFields[i]
		}
	}
	return nil
}

// wrapperKeyCard is the key the backend sends a wrapper behind a gateway or
// proxy, such as a Cloudflare Worker: empty for a wrapper on the LAN.
func (a *App) wrapperKeyCard(c *ui.Context, k *Tokens) {
	f := a.wrapperKeyField()
	if f == nil {
		return
	}
	label(c, k, "Wrapper key")
	ui.Column(c.Key("wrapper-key")).Padding(4, padCard).Radius(radiusCard).Background(k.Raise).Children(func() {
		a.fieldRow(c, k, f)
	})
}

// newWrapperKey is a strong random key, as amdl wrapper keygen prints;
// "" when the system has no randomness to give.
func newWrapperKey() string {
	key, err := wrapper.GenerateKey()
	if err != nil {
		return ""
	}
	return key
}
