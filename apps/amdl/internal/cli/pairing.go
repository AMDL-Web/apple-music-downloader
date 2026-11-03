package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"amdl/internal/capability"
	"amdl/internal/client"
)

// pairPollEvery is how often amdl wrapper pair asks the backend whether the
// wrapper's owner has approved.
var pairPollEvery = 2 * time.Second

// pairGrace is how long past the code's expires_at the command keeps asking,
// so the backend's own verdict on an expired code arrives before the command
// gives up on its own clock.
var pairGrace = 10 * time.Second

// wrapperPairCmd is amdl wrapper pair: start a device-code pairing, show the
// code and the page to approve it on, then wait for the answer. --status only
// reads the state.
func wrapperPairCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	status := fs.Bool("status", false, "only show the pairing state; start nothing")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl wrapper pair [--status]")
		}
		opID := "startWrapperPairing"
		if *status {
			opID = "getWrapperPairing"
		}
		if e.dry || *status {
			req, err := capability.Lookup(opID).Build(nil)
			if err != nil {
				return usageErr("%v", err)
			}
			return e.send(req, nil)
		}
		p, err := e.c.StartWrapperPairing(e.ctx)
		if err != nil {
			return err
		}
		if e.ErrTTY {
			// The code is the point of the command, so --quiet keeps it.
			link := p.VerificationURLComplete
			if link == "" {
				link = p.VerificationURL
			}
			fmt.Fprintf(e.Stderr, "Open %s and approve code %s\n", link, p.UserCode)
			e.note("Waiting for the approval; Ctrl-C stops waiting and the pairing goes on in the backend.")
		} else if err := e.out.line(p); err != nil {
			return err
		}
		final, err := e.awaitPairing(p)
		if err != nil {
			return err
		}
		if e.ErrTTY {
			return e.out.result(final)
		}
		return e.out.line(final)
	}
}

// awaitPairing polls the backend until the pairing leaves pending, bounded by
// the code's expiry. Anything but paired is an error.
func (e *env) awaitPairing(p client.WrapperPairing) (client.WrapperPairing, error) {
	ctx := e.ctx
	if p.ExpiresAt != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(e.ctx, p.ExpiresAt.Add(pairGrace))
		defer cancel()
	}
	for p.State == client.PairingPending {
		select {
		case <-ctx.Done():
			if e.ctx.Err() != nil {
				return p, e.ctx.Err()
			}
			return p, &cliError{Code: "wait_timeout", exit: exitWaitTimeout,
				Message: "the pairing code expired without an approval", Hint: "amdl wrapper pair"}
		case <-time.After(pairPollEvery):
		}
		next, err := e.c.WrapperPairing(ctx)
		switch {
		case err == nil:
			p = next
		case ctx.Err() != nil && !errors.Is(err, e.ctx.Err()):
			continue // the deadline's own cancellation; the select above reports it
		default:
			return p, err
		}
	}
	if p.State != client.PairingPaired {
		msg := p.LastError
		if msg == "" {
			msg = "the wrapper did not approve the pairing"
		}
		return p, &cliError{Code: "pairing_failed", exit: exitAuth, Message: msg, Hint: "amdl wrapper pair"}
	}
	return p, nil
}

// wrapperUnpairCmd is amdl wrapper unpair: forget the stored credential.
func wrapperUnpairCmd(*flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("deleteWrapperPairing")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl wrapper unpair")
		}
		req, err := op.Build(nil)
		if err != nil {
			return usageErr("%v", err)
		}
		return e.send(req, nil)
	}
}
