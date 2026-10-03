package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"private-release/internal/domain"
)

var ErrNotFound = errors.New("not found")
var ErrAlreadyExists = errors.New("already exists")
var ErrBudgetExceeded = errors.New("privacy budget exceeded")

type QueryResult struct {
	Operation domain.Operation `json:"operation"`
	Epsilon   string           `json:"epsilon"`
	Spent     string           `json:"spent_epsilon"`
	Remaining string           `json:"remaining_epsilon"`
	Result    any              `json:"result"`
}

type Mechanism func(records []domain.Record, project LoadedProject, epsilon domain.Decimal) (any, error)

type LoadedProject struct {
	ID     string
	Lower  int64
	Upper  int64
	Bins   []domain.Bin
	Budget domain.Decimal
}

type DB struct{ sql *sql.DB }

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error { return db.sql.Close() }

func (db *DB) migrate() error {
	_, err := db.sql.Exec(`
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  lower INTEGER NOT NULL,
  upper INTEGER NOT NULL,
  edges_json TEXT NOT NULL,
  budget_text TEXT NOT NULL,
  spent_text TEXT NOT NULL DEFAULT '0'
);
CREATE TABLE IF NOT EXISTS records (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  value INTEGER NOT NULL,
  PRIMARY KEY(project_id, user_id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS query_results (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  epsilon_text TEXT NOT NULL,
  result_json TEXT NOT NULL,
  PRIMARY KEY(project_id, operation, epsilon_text)
) WITHOUT ROWID;`)
	return err
}

func (db *DB) CreateProject(in *domain.CreateProjectInput) error {
	records, _, budget, err := domain.ValidateCreate(in)
	if err != nil {
		return err
	}
	edges, err := json.Marshal(in.Edges)
	if err != nil {
		return err
	}
	tx, err := db.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO projects(id, lower, upper, edges_json, budget_text) VALUES (?, ?, ?, ?, ?)`,
		in.ID, in.Lower, in.Upper, string(edges), budget.String())
	if err != nil {
		if isUnique(err) {
			return fmt.Errorf("project %q: %w", in.ID, ErrAlreadyExists)
		}
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO records(project_id, user_id, value) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, record := range records {
		if _, err := stmt.Exec(in.ID, record.UserID, record.Value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) Project(id string) (LoadedProject, domain.Decimal, error) {
	return loadProject(context.Background(), db.sql, id)
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (db *DB) Publish(ctx context.Context, projectID string, op domain.Operation, epsilon domain.Decimal, mechanism Mechanism) (QueryResult, error) {
	tx, err := db.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return QueryResult{}, err
	}
	defer tx.Rollback()

	project, spent, err := loadProject(ctx, tx, projectID)
	if err != nil {
		return QueryResult{}, err
	}
	if result, ok, err := findResult(ctx, tx, projectID, op, epsilon, project.Budget, spent); err != nil || ok {
		if err == nil {
			err = tx.Commit()
		}
		return result, err
	}
	newSpent := spent.Add(epsilon)
	if newSpent.Cmp(project.Budget) > 0 {
		return QueryResult{}, fmt.Errorf("%w: requested %s, spent %s, total budget %s", ErrBudgetExceeded, epsilon, spent, project.Budget)
	}
	records, err := loadRecords(ctx, tx, projectID)
	if err != nil {
		return QueryResult{}, err
	}
	value, err := mechanism(records, project, epsilon)
	if err != nil {
		return QueryResult{}, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return QueryResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO query_results(project_id, operation, epsilon_text, result_json) VALUES (?, ?, ?, ?)`,
		projectID, string(op), epsilon.String(), string(encoded)); err != nil {
		return QueryResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET spent_text = ? WHERE id = ?`, newSpent.String(), projectID); err != nil {
		return QueryResult{}, err
	}
	result := QueryResult{Operation: op, Epsilon: epsilon.String(), Spent: newSpent.String(), Remaining: project.Budget.Sub(newSpent).String(), Result: json.RawMessage(encoded)}
	return result, tx.Commit()
}

func loadProject(ctx context.Context, q rowQuerier, id string) (LoadedProject, domain.Decimal, error) {
	var project LoadedProject
	var edgesJSON, budgetText, spentText string
	err := q.QueryRowContext(ctx, `SELECT id, lower, upper, edges_json, budget_text, spent_text FROM projects WHERE id = ?`, id).
		Scan(&project.ID, &project.Lower, &project.Upper, &edgesJSON, &budgetText, &spentText)
	if errors.Is(err, sql.ErrNoRows) {
		return LoadedProject{}, domain.Decimal{}, ErrNotFound
	}
	if err != nil {
		return LoadedProject{}, domain.Decimal{}, err
	}
	var edges []int64
	if err := json.Unmarshal([]byte(edgesJSON), &edges); err != nil {
		return LoadedProject{}, domain.Decimal{}, err
	}
	project.Bins, err = domain.ValidateBins(project.Lower, project.Upper, edges)
	if err != nil {
		return LoadedProject{}, domain.Decimal{}, err
	}
	project.Budget, err = domain.ParseDecimal(budgetText)
	if err != nil {
		return LoadedProject{}, domain.Decimal{}, err
	}
	spent, err := domain.ParseNonNegativeDecimal(spentText)
	if err != nil {
		return LoadedProject{}, domain.Decimal{}, err
	}
	return project, spent, nil
}

func loadRecords(ctx context.Context, tx *sql.Tx, projectID string) ([]domain.Record, error) {
	rows, err := tx.QueryContext(ctx, `SELECT user_id, value FROM records WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []domain.Record
	for rows.Next() {
		var record domain.Record
		if err := rows.Scan(&record.UserID, &record.Value); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func findResult(ctx context.Context, tx *sql.Tx, projectID string, op domain.Operation, epsilon, budget, spent domain.Decimal) (QueryResult, bool, error) {
	var encoded string
	err := tx.QueryRowContext(ctx, `SELECT result_json FROM query_results WHERE project_id=? AND operation=? AND epsilon_text=?`,
		projectID, string(op), epsilon.String()).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return QueryResult{}, false, nil
	}
	if err != nil {
		return QueryResult{}, false, err
	}
	return QueryResult{Operation: op, Epsilon: epsilon.String(), Spent: spent.String(), Remaining: budget.Sub(spent).String(), Result: json.RawMessage(encoded)}, true, nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint")
}
