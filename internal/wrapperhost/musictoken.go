package wrapperhost

import (
	"context"
	"errors"
)

// ErrMultipleAccounts requires the per-account provider to select an instance.
var ErrMultipleAccounts = errors.New("the local wrapper has several accounts; select a manager instance")

// MusicToken serves callers with the scalar token interface until they adopt
// Instances and InstanceToken. It refuses to select between several accounts.
func (h Home) MusicToken() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), LoginLimit())
	defer cancel()
	instances, err := h.Instances(ctx)
	if errors.Is(err, ErrNotInstalled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(instances) > 1 {
		return "", ErrMultipleAccounts
	}
	if len(instances) == 0 || !instances[0].Ready || instances[0].Degraded {
		return "", nil
	}
	reply, err := h.InstanceToken(ctx, instances[0].ID)
	return reply.MusicToken, err
}
