package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"amdl/internal/domain"
)

// ErrAccountNotFound is returned when no account has the id.
var ErrAccountNotFound = errors.New("account not found")

const accountColumns = `id, name, region, position, library_sync, apple_library, state, added_at, seen_at`

func scanAccount(row jobScanner) (domain.Account, error) {
	var (
		a            domain.Account
		state, added string
		seen         sql.NullString
	)
	if err := row.Scan(&a.ID, &a.Name, &a.Region, &a.Position, &a.LibrarySync, &a.AppleLibrary, &state, &added, &seen); err != nil {
		return domain.Account{}, err
	}
	a.State = domain.AccountState(state)
	a.AddedAt = parseTime(added)
	a.SeenAt = parseOptionalTime(seen)
	return a, nil
}

// ListAccounts returns every account, the default (position 0) first.
func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	return listAccounts(ctx, s.db)
}

func listAccounts(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}) ([]domain.Account, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY position, added_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount returns the account with id, or ErrAccountNotFound.
func (s *Store) GetAccount(ctx context.Context, id string) (domain.Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, ErrAccountNotFound
	}
	return a, err
}

// SeenAccount is one account a discovery found.
type SeenAccount struct {
	ID     string
	Name   string
	Region string
}

// AccountDiscovery is one discovery's result, applied by SyncAccounts.
type AccountDiscovery struct {
	// Seen are the accounts the wrapper reports, in its order.
	Seen []SeenAccount
	// Complete says Seen is every account the wrapper holds, so an account
	// not in it is marked missing. A discovery that could not tell (the
	// wrapper did not answer) leaves the states alone.
	Complete bool
	// Adopt is the account that takes over the rows without an account
	// (account_id '', the anchor and Apple library cache of a database from
	// before accounts) when the table has no account yet. It becomes the
	// default, with its library-sync switch set to AdoptLibrarySync and its
	// Apple library switch on, as the single account had them. Empty adopts
	// the first of Seen.
	Adopt            string
	AdoptLibrarySync bool
	Now              time.Time
}

// AccountsSync reports what SyncAccounts changed.
type AccountsSync struct {
	// Added are the accounts seen for the first time.
	Added []string
	// Adopted is the account that took over the rows without an account;
	// empty when there was no adoption.
	Adopted string
}

// SyncAccounts applies a discovery in one transaction. A known account takes
// the name and region the wrapper reports (an empty one keeps the stored
// value) and becomes active; a new one is appended after the others with both
// switches off, except the first account of an empty table, which adopts the
// rows without an account (AccountDiscovery.Adopt). With Complete, an account
// the discovery did not report is marked missing; it keeps its row, position
// and switches.
func (s *Store) SyncAccounts(ctx context.Context, d AccountDiscovery) (AccountsSync, error) {
	if d.Now.IsZero() {
		d.Now = now()
	}
	stamp := formatTime(d.Now)
	return withTx(ctx, s.db, func(tx *sql.Tx) (AccountsSync, error) {
		var out AccountsSync
		existing, err := listAccounts(ctx, tx)
		if err != nil {
			return out, err
		}
		known := make(map[string]domain.Account, len(existing))
		next := 0
		for _, a := range existing {
			known[a.ID] = a
			next = max(next, a.Position+1)
		}
		adopt := ""
		if len(existing) == 0 && len(d.Seen) > 0 {
			adopt = d.Seen[0].ID
			for _, seen := range d.Seen {
				if seen.ID == d.Adopt {
					adopt = d.Adopt
				}
			}
		}
		seenIDs := make(map[string]bool, len(d.Seen))
		if adopt != "" {
			next = 1
		}
		for _, seen := range d.Seen {
			if seen.ID == "" || seenIDs[seen.ID] {
				continue
			}
			seenIDs[seen.ID] = true
			region := strings.ToLower(strings.TrimSpace(seen.Region))
			if _, ok := known[seen.ID]; ok {
				if _, err := tx.ExecContext(ctx, `UPDATE accounts SET
					name = CASE WHEN ? <> '' THEN ? ELSE name END,
					region = CASE WHEN ? <> '' THEN ? ELSE region END,
					state = 'active', seen_at = ? WHERE id = ?`,
					seen.Name, seen.Name, region, region, stamp, seen.ID); err != nil {
					return out, fmt.Errorf("update account %s: %w", seen.ID, err)
				}
				continue
			}
			position, librarySync, appleLibrary := next, false, false
			if seen.ID == adopt {
				position, librarySync, appleLibrary = 0, d.AdoptLibrarySync, true
			} else {
				next++
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO accounts(`+accountColumns+`) VALUES(?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
				seen.ID, seen.Name, region, position, librarySync, appleLibrary, stamp, stamp); err != nil {
				return out, fmt.Errorf("add account %s: %w", seen.ID, err)
			}
			out.Added = append(out.Added, seen.ID)
		}
		if adopt != "" {
			if err := adoptUnowned(ctx, tx, adopt); err != nil {
				return out, err
			}
			out.Adopted = adopt
		}
		if d.Complete {
			for _, a := range existing {
				if !seenIDs[a.ID] && a.State != domain.AccountMissing {
					if _, err := tx.ExecContext(ctx, `UPDATE accounts SET state = 'missing' WHERE id = ?`, a.ID); err != nil {
						return out, fmt.Errorf("mark account %s missing: %w", a.ID, err)
					}
				}
			}
		}
		return out, nil
	})
}

// accountTables are the tables whose rows belong to one account.
var accountTables = []string{"library_sync_anchor", "apple_library_items", "apple_library_playlist_tracks", "apple_library_meta"}

// adoptUnowned hands the rows without an account to id.
func adoptUnowned(ctx context.Context, tx *sql.Tx, id string) error {
	for _, table := range accountTables {
		// A row id already has (none, for a new account) wins over the
		// unowned copy.
		if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE `+table+` SET account_id = ? WHERE account_id = ''`, id); err != nil {
			return fmt.Errorf("adopt %s rows: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE account_id = ''`); err != nil {
			return fmt.Errorf("adopt %s rows: %w", table, err)
		}
	}
	return nil
}

// AccountPatch changes an account's switches or position; a nil field stays.
type AccountPatch struct {
	LibrarySync  *bool
	AppleLibrary *bool
	// Position moves the account there, shifting the ones between; 0 makes
	// it the default. A position past the end moves it last.
	Position *int
}

// UpdateAccount applies p to the account with id and returns it.
func (s *Store) UpdateAccount(ctx context.Context, id string, p AccountPatch) (domain.Account, error) {
	return withTx(ctx, s.db, func(tx *sql.Tx) (domain.Account, error) {
		accounts, err := listAccounts(ctx, tx)
		if err != nil {
			return domain.Account{}, err
		}
		from := -1
		for i, a := range accounts {
			if a.ID == id {
				from = i
			}
		}
		if from < 0 {
			return domain.Account{}, ErrAccountNotFound
		}
		if p.LibrarySync != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET library_sync = ? WHERE id = ?`, *p.LibrarySync, id); err != nil {
				return domain.Account{}, err
			}
		}
		if p.AppleLibrary != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET apple_library = ? WHERE id = ?`, *p.AppleLibrary, id); err != nil {
				return domain.Account{}, err
			}
		}
		if p.Position != nil {
			to := min(max(*p.Position, 0), len(accounts)-1)
			moved := accounts[from]
			order := append(append([]domain.Account{}, accounts[:from]...), accounts[from+1:]...)
			order = append(order[:to], append([]domain.Account{moved}, order[to:]...)...)
			if err := renumberAccounts(ctx, tx, order); err != nil {
				return domain.Account{}, err
			}
		}
		a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id))
		return a, err
	})
}

// renumberAccounts stores order's positions as 0, 1, 2 and so on.
func renumberAccounts(ctx context.Context, tx *sql.Tx, order []domain.Account) error {
	for i, a := range order {
		if a.Position == i {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET position = ? WHERE id = ?`, i, a.ID); err != nil {
			return fmt.Errorf("move account %s: %w", a.ID, err)
		}
	}
	return nil
}

// DeleteAccount removes the account with id and everything amdl keeps for
// it alone: its library-sync anchor and its Apple library cache. Records,
// files and runs stay. The accounts after it move up, so a deleted default
// hands that role to the next account.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	_, err := withTx(ctx, s.db, func(tx *sql.Tx) (struct{}, error) {
		res, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
		if err != nil {
			return struct{}{}, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return struct{}{}, err
		} else if n == 0 {
			return struct{}{}, ErrAccountNotFound
		}
		for _, table := range accountTables {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE account_id = ?`, id); err != nil {
				return struct{}{}, fmt.Errorf("delete the %s rows of account %s: %w", table, id, err)
			}
		}
		rest, err := listAccounts(ctx, tx)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, renumberAccounts(ctx, tx, rest)
	})
	return err
}
