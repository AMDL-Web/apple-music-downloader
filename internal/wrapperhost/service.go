package wrapperhost

import (
	"context"

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

// Status reports the guest; while it boots or is stopped, the answer is
// "not ready" with the supervisor's state rather than an error.
func (s Service) Status(ctx context.Context) (wrapper.Status, error) {
	st, err := s.Wrapper.Status(ctx)
	if err != nil {
		st = wrapper.Status{Regions: []string{}, Accounts: []string{}}
	}
	st.LoginSupported = true
	m := s.Sup.Managed()
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
