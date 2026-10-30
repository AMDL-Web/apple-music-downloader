package wrapperhost

import (
	"context"
	"time"

	"amdl/internal/wrapper"
)

// Service is the backend's wrapper service for a managed wrapper: status
// from the guest's own lite, marked as managed and as able to log in, and
// login, two-step and logout through the supervisor.
type Service struct {
	Wrapper interface {
		Status(context.Context) (wrapper.Status, error)
	}
	Sup *Supervisor
}

// Status reports the guest; while it is not serving, or does not answer
// within a few seconds, the answer is "not ready" with the supervisor's
// state rather than an error. A booting guest's port can take a connection
// and never answer it.
func (s Service) Status(ctx context.Context) (wrapper.Status, error) {
	m := s.Sup.Managed()
	st := wrapper.Status{Regions: []string{}, Accounts: []string{}}
	if m.Mode == "serving" {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if got, err := s.Wrapper.Status(ctx); err == nil {
			st = got
		}
	}
	st.LoginSupported = true
	st.Managed = &m
	return st, nil
}

// StartLogin implements the backend's wrapper service.
func (s Service) StartLogin(ctx context.Context, user, pass string) (wrapper.LoginResult, error) {
	return s.Sup.StartLogin(ctx, user, pass)
}

// SubmitTwoStepCode implements the backend's wrapper service.
func (s Service) SubmitTwoStepCode(ctx context.Context, id, code string) (wrapper.LoginResult, error) {
	return s.Sup.SubmitTwoStepCode(ctx, id, code)
}

// Logout implements the backend's wrapper service.
func (s Service) Logout(ctx context.Context, user string) error { return s.Sup.Logout(ctx, user) }
