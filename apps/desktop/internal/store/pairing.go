package store

import (
	"context"
	"time"

	"amdl/internal/client"
)

// Pair is a wrapper pairing on its way: the request out, why the last one
// failed, and whether the wrapper takes requests without one.
type Pair struct {
	// Busy is set while a start or an unpair waits for its answer, which
	// left at Since.
	Busy  bool
	Since time.Time
	// Err is why the last request failed.
	Err string
	// Unsupported means the wrapper needs no pairing (501
	// pairing_unsupported), so there is nothing to connect.
	Unsupported bool
	// polling counts the polls of a pending pairing that run.
	polling int
}

// PairingPoll is how often a pending pairing is read; pairingGrace is how
// long past its expiry the poll goes on, for the backend to report it.
var (
	PairingPoll  = 2 * time.Second
	pairingGrace = 5 * time.Second
	// pairingLongest bounds a pending pairing that names no expiry.
	pairingLongest = 15 * time.Minute
)

// StartPairing asks the backend to pair with the wrapper's gateway, then
// follows the pairing until its owner approves or it ends.
func (s *Store) StartPairing() {
	p := &s.Pair
	if s.client == nil || p.Busy {
		return
	}
	p.Busy, p.Since = true, s.now()
	c, ctx := s.client, s.ctx
	go func() {
		st, err := c.StartWrapperPairing(ctx)
		s.postIf(ctx, func() {
			p.Busy, p.Err = false, ""
			switch {
			case client.ErrorCode(err) == "pairing_unsupported":
				p.Unsupported = true
			case err != nil:
				p.Err = Reason(err)
			default:
				s.Server.Pairing = &st
				s.followPairing(st)
			}
		})
	}()
}

// followPairing reads a pending pairing every PairingPoll until it leaves
// pending, or its code expired a little while ago.
func (s *Store) followPairing(st client.WrapperPairing) {
	if st.State != client.PairingPending || s.Pair.polling > 0 {
		return
	}
	end := s.now().Add(pairingLongest)
	if st.ExpiresAt != nil {
		end = st.ExpiresAt.Add(pairingGrace)
	}
	c, ctx := s.client, s.ctx
	s.Pair.polling++
	go func() {
		defer s.postIf(ctx, func() { s.Pair.polling-- })
		t := time.NewTicker(PairingPoll)
		defer t.Stop()
		stop := time.NewTimer(time.Until(end))
		defer stop.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop.C:
				s.postIf(ctx, s.refreshServer)
				return
			case <-t.C:
			}
			rctx, cancel := context.WithTimeout(ctx, PairingPoll*5)
			v, err := c.WrapperPairing(rctx)
			cancel()
			if err != nil {
				continue
			}
			done := v.State != client.PairingPending
			s.postIf(ctx, func() {
				s.Server.Pairing = &v
				if done {
					s.refreshAccounts()
				}
			})
			if done {
				return
			}
		}
	}()
}

// Unpair forgets the wrapper pairing, or the pending one.
func (s *Store) Unpair() {
	p := &s.Pair
	if s.client == nil || p.Busy {
		return
	}
	p.Busy, p.Since = true, s.now()
	c, ctx := s.client, s.ctx
	go func() {
		st, err := c.UnpairWrapper(ctx)
		s.postIf(ctx, func() {
			p.Busy, p.Err = false, ""
			if err != nil {
				p.Err = Reason(err)
				return
			}
			s.Server.Pairing = &st
		})
	}()
}
