// Package store persists projects, contributions, released query results
// and privacy budget accounting in SQLite. Budget check, result save and
// budget deduction happen in a single atomic transaction.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"

	"private-release/internal/decimal"
)

// Project is an immutable analysis project.
type Project struct {
	ID        string
	Lower     int64
	Upper     int64
	Bins      int64
	Budget    decimal.Value
	BudgetKey string
}

// ErrDuplicateUser is returned when a user ID contributes twice.
var ErrDuplicateUser = errors.New("user already contributed to this project")

// ErrProjectExists is returned when re-creating a project ID.
var ErrProjectExists = errors.New("project already exists")

// ErrNotFound is returned for unknown projects.
var ErrNotFound = errors.New("project not found")

// ErrBudgetExhausted is returned when a query would exceed the budget.
var ErrBudgetExhausted = errors.New("insufficient privacy budget")

type Store struct {
	db *sql.DB
	// mu serializes publish transactions per process so concurrent identical
	// queries publish exactly once and distinct queries cannot overspend.
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Publish is serialized by s.mu, but compute callbacks query through the
	// same pool while a transaction holds a connection, so allow several.
	db.SetMaxOpenConns(8)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  lower INTEGER NOT NULL,
  upper INTEGER NOT NULL,
  bins INTEGER NOT NULL,
  budget_key TEXT NOT NULL,
  budget_display TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS contributions (
  project_id TEXT NOT NULL REFERENCES projects(id),
  user_id TEXT NOT NULL,
  value INTEGER NOT NULL,
  PRIMARY KEY (project_id, user_id)
);
CREATE TABLE IF NOT EXISTS releases (
  project_id TEXT NOT NULL REFERENCES projects(id),
  op TEXT NOT NULL,
  eps_key TEXT NOT NULL,
  eps_display TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (project_id, op, eps_key)
);`)
	return err
}

// CreateProject inserts a new immutable project; IDs cannot be reused.
func (s *Store) CreateProject(p Project) error {
	_, err := s.db.Exec(
		`INSERT INTO projects(id, lower, upper, bins, budget_key, budget_display) VALUES(?,?,?,?,?,?)`,
		p.ID, p.Lower, p.Upper, p.Bins, p.Budget.Key(), p.Budget.String())
	if err != nil {
		if isUniqueViolation(err) {
			return ErrProjectExists
		}
		return err
	}
	return nil
}

func (s *Store) GetProject(id string) (Project, error) {
	var p Project
	var budgetKey, budgetDisplay string
	err := s.db.QueryRow(`SELECT id, lower, upper, bins, budget_key, budget_display FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Lower, &p.Upper, &p.Bins, &budgetKey, &budgetDisplay)
	if err == sql.ErrNoRows {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	b, err := decimal.FromKey(budgetKey)
	if err != nil {
		return Project{}, fmt.Errorf("corrupt budget for project %s: %w", id, err)
	}
	p.Budget = b
	p.BudgetKey = budgetKey
	return p, nil
}

// AddContribution stores one clamped contribution per user ID.
func (s *Store) AddContribution(projectID, userID string, value int64) error {
	_, err := s.db.Exec(`INSERT INTO contributions(project_id, user_id, value) VALUES(?,?,?)`,
		projectID, userID, value)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateUser
		}
	}
	return err
}

// Contributions returns all clamped values of a project (internal use only;
// never exposed through the public API).
func (s *Store) Contributions(projectID string) ([]int64, error) {
	rows, err := s.db.Query(`SELECT value FROM contributions WHERE project_id=?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PublishOutcome describes the result of a publish attempt.
type PublishOutcome struct {
	Result    []byte // JSON-encoded noisy result
	Reused    bool
	Spent     decimal.Value
	Remaining decimal.Value
}

// Publish atomically checks the budget, stores the noisy result and deducts
// epsilon. If the same (op, epsilon) query was already released, the stored
// result is returned without re-sampling or charging. compute is invoked
// inside the transaction only when a fresh release is needed.
func (s *Store) Publish(ctx context.Context, projectID, op string, eps decimal.Value, compute func() ([]byte, error)) (PublishOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishOutcome{}, err
	}
	defer tx.Rollback()

	var budgetKey string
	err = tx.QueryRow(`SELECT budget_key FROM projects WHERE id=?`, projectID).Scan(&budgetKey)
	if err == sql.ErrNoRows {
		return PublishOutcome{}, ErrNotFound
	}
	if err != nil {
		return PublishOutcome{}, err
	}
	budget, err := decimal.FromKey(budgetKey)
	if err != nil {
		return PublishOutcome{}, err
	}

	// Reuse: identical (op, numerically-equal epsilon) query already released.
	var result []byte
	var epsDisplay string
	err = tx.QueryRow(`SELECT result, eps_display FROM releases WHERE project_id=? AND op=? AND eps_key=?`,
		projectID, op, eps.Key()).Scan(&result, &epsDisplay)
	if err != nil && err != sql.ErrNoRows {
		return PublishOutcome{}, err
	}
	reused := err == nil

	rows, err := tx.Query(`SELECT eps_key FROM releases WHERE project_id=?`, projectID)
	if err != nil {
		return PublishOutcome{}, err
	}
	var epsList []decimal.Value
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return PublishOutcome{}, err
		}
		v, perr := decimal.FromKey(d)
		if perr != nil {
			rows.Close()
			return PublishOutcome{}, perr
		}
		epsList = append(epsList, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PublishOutcome{}, err
	}
	spent := decimal.Sum(epsList)

	if !reused {
		if decimal.Add(spent, eps).Cmp(budget) > 0 {
			return PublishOutcome{}, fmt.Errorf("%w: query epsilon %s would bring spend to %s, budget is %s (remaining %s)",
				ErrBudgetExhausted, eps.String(), decimal.Add(spent, eps).String(), budget.String(), decimal.Sub(budget, spent).String())
		}
		result, err = compute()
		if err != nil {
			return PublishOutcome{}, err
		}
		if _, err := tx.Exec(`INSERT INTO releases(project_id, op, eps_key, eps_display, result) VALUES(?,?,?,?,?)`,
			projectID, op, eps.Key(), eps.String(), result); err != nil {
			return PublishOutcome{}, err
		}
		spent = decimal.Add(spent, eps)
	}

	if err := tx.Commit(); err != nil {
		return PublishOutcome{}, err
	}
	return PublishOutcome{
		Result:    result,
		Reused:    reused,
		Spent:     spent,
		Remaining: decimal.Sub(budget, spent),
	}, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE constraint failed") || contains(err.Error(), "constraint failed"))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
