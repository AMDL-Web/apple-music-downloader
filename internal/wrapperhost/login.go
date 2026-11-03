package wrapperhost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"amdl/internal/wrapper"
)

// ErrStopped refuses account changes while the local manager is stopped.
var ErrStopped = fmt.Errorf("%w: the local wrapper is stopped; amdl wrapper start turns it on", wrapper.ErrUnavailable)

// ErrAccountLimit means the local manager already holds MaxAccounts accounts.
var ErrAccountLimit = errors.New("the local wrapper holds at most 10 accounts; log one out first")

func (s *Supervisor) accountChangesAllowed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.home.Enabled() || s.run.Err() != nil || s.st.Mode != "serving" {
		return ErrStopped
	}
	return nil
}

// StartLogin logs in through the same manager API used by a remote wrapper.
func (s *Supervisor) StartLogin(ctx context.Context, user, pass string) (wrapper.LoginResult, error) {
	ctx, cancel := context.WithTimeout(ctx, LoginLimit())
	defer cancel()
	if err := s.accountChangesAllowed(); err != nil {
		return wrapper.LoginResult{}, err
	}
	instances, err := s.home.Instances(ctx)
	if err != nil {
		return wrapper.LoginResult{}, err
	}
	if len(instances) >= MaxAccounts {
		// An existing account may be reauthenticated at the limit.
		found := false
		for _, in := range instances {
			if in.Name == user {
				found = true
				break
			}
		}
		if !found {
			return wrapper.LoginResult{}, ErrAccountLimit
		}
	}
	return s.manager.StartLogin(ctx, user, pass)
}

var twoStepCode = regexp.MustCompile(`^[0-9]{6}$`)

// ErrBadTwoStepCode leaves a pending login available for a valid code.
var ErrBadTwoStepCode = fmt.Errorf("%w: a two-step code is six digits", wrapper.ErrInvalidTwoStepCode)

// SubmitTwoStepCode completes the manager login without restarting the guest.
func (s *Supervisor) SubmitTwoStepCode(ctx context.Context, id, code string) (wrapper.LoginResult, error) {
	ctx, cancel := context.WithTimeout(ctx, LoginLimit())
	defer cancel()
	if err := s.accountChangesAllowed(); err != nil {
		return wrapper.LoginResult{}, err
	}
	if !twoStepCode.MatchString(code) {
		return wrapper.LoginResult{}, ErrBadTwoStepCode
	}
	return s.manager.SubmitTwoStepCode(ctx, id, code)
}

// Logout removes only the manager instance with id, leaving other accounts intact.
func (s *Supervisor) Logout(ctx context.Context, id string) error {
	if err := s.accountChangesAllowed(); err != nil {
		return err
	}
	return managerCall(ctx, s.URL(), http.MethodPost, "/logout", map[string]string{"id": id}, nil)
}
