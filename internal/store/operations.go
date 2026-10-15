// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Operation statuses.
const (
	OpPlanned   = "planned"
	OpPending   = "pending"
	OpRunning   = "running"
	OpPaused    = "paused"
	OpSucceeded = "succeeded"
	OpFailed    = "failed"
	OpCancelled = "cancelled"
)

// Step statuses.
const (
	StepPending   = "pending"
	StepRunning   = "running"
	StepSucceeded = "succeeded"
	StepFailed    = "failed"
	StepSkipped   = "skipped"
)

// Operation is a durable, multi-step change to a cluster (ADR 0008).
type Operation struct {
	ID              string          `json:"id"`
	ClusterID       string          `json:"clusterId"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	Summary         string          `json:"summary"`
	Params          json.RawMessage `json:"params"`
	Plan            json.RawMessage `json:"plan"`
	Irreversible    bool            `json:"irreversible"`
	CurrentStep     int             `json:"currentStep"`
	RequestedAction string          `json:"requestedAction"`
	Error           string          `json:"error"`
	CreatedBy       string          `json:"createdBy"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       *time.Time      `json:"startedAt"`
	FinishedAt      *time.Time      `json:"finishedAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Steps           []*Step         `json:"steps,omitempty"`
}

// Step is one idempotent action of an operation.
type Step struct {
	Index      int             `json:"index"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Args       json.RawMessage `json:"args"`
	AlwaysRun  bool            `json:"alwaysRun"`
	Status     string          `json:"status"`
	Detail     json.RawMessage `json:"detail"`
	Error      string          `json:"error"`
	StartedAt  *time.Time      `json:"startedAt"`
	FinishedAt *time.Time      `json:"finishedAt"`
}

const opColumns = `id, cluster_id, type, status, summary, params, plan, irreversible, current_step, requested_action,
	error, created_by, created_at, started_at, finished_at, updated_at`

func scanOperation(row scanner) (*Operation, error) {
	var o Operation
	if err := row.Scan(&o.ID, &o.ClusterID, &o.Type, &o.Status, &o.Summary, &o.Params, &o.Plan, &o.Irreversible,
		&o.CurrentStep, &o.RequestedAction, &o.Error, &o.CreatedBy, &o.CreatedAt, &o.StartedAt, &o.FinishedAt,
		&o.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &o, nil
}

// CreateOperation inserts an operation with its steps in one transaction.
func (s *Store) CreateOperation(ctx context.Context, o *Operation) error {
	if o.ID == "" {
		o.ID = NewID()
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	err = tx.QueryRow(ctx, `INSERT INTO operations (id, cluster_id, type, status, summary, params, plan, irreversible,
		created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING created_at, updated_at`,
		o.ID, o.ClusterID, o.Type, o.Status, o.Summary, jsonOrEmpty(o.Params), jsonOrEmpty(o.Plan), o.Irreversible,
		o.CreatedBy).Scan(&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return conflict(err)
	}
	for i, st := range o.Steps {
		st.Index = i
		if st.Status == "" {
			st.Status = StepPending
		}
		if _, err := tx.Exec(ctx, `INSERT INTO operation_steps (operation_id, idx, name, kind, args, always_run, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, o.ID, i, st.Name, st.Kind, jsonOrEmpty(st.Args), st.AlwaysRun, st.Status); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetOperation returns an operation with its steps.
func (s *Store) GetOperation(ctx context.Context, id string) (*Operation, error) {
	o, err := scanOperation(s.Pool.QueryRow(ctx, `SELECT `+opColumns+` FROM operations WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT idx, name, kind, args, always_run, status, detail, error, started_at, finished_at
		FROM operation_steps WHERE operation_id=$1 ORDER BY idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st Step
		if err := rows.Scan(&st.Index, &st.Name, &st.Kind, &st.Args, &st.AlwaysRun, &st.Status, &st.Detail, &st.Error,
			&st.StartedAt, &st.FinishedAt); err != nil {
			return nil, err
		}
		o.Steps = append(o.Steps, &st)
	}
	return o, rows.Err()
}

// ListOperations returns recent operations, optionally for one cluster.
func (s *Store) ListOperations(ctx context.Context, clusterID string, limit int) ([]*Operation, error) {
	var rows pgx.Rows
	var err error
	if clusterID == "" {
		rows, err = s.Pool.Query(ctx, `SELECT `+opColumns+` FROM operations ORDER BY created_at DESC LIMIT $1`, limit)
	} else {
		rows, err = s.Pool.Query(ctx, `SELECT `+opColumns+` FROM operations WHERE cluster_id=$1
			ORDER BY created_at DESC LIMIT $2`, clusterID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Operation
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetOperationStatus changes the status of an operation; terminal statuses set finished_at.
func (s *Store) SetOperationStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operations SET status=$2, error=$3, updated_at=now(),
		started_at = CASE WHEN $2 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
		finished_at = CASE WHEN $2 IN ('succeeded','failed','cancelled') THEN now() ELSE finished_at END
		WHERE id=$1`, id, status, errMsg)
	return conflict(err)
}

// SetRequestedAction records a pause or cancel request from a user.
func (s *Store) SetRequestedAction(ctx context.Context, id, action string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operations SET requested_action=$2, updated_at=now() WHERE id=$1`, id, action)
	return err
}

// SetCurrentStep records the step the engine is executing.
func (s *Store) SetCurrentStep(ctx context.Context, id string, idx int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operations SET current_step=$2, updated_at=now() WHERE id=$1`, id, idx)
	return err
}

// UpdateStep stores the status, detail and error of a step.
func (s *Store) UpdateStep(ctx context.Context, opID string, idx int, status string, detail json.RawMessage, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE operation_steps SET status=$3, detail=$4, error=$5,
		started_at = CASE WHEN $3 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
		finished_at = CASE WHEN $3 IN ('succeeded','failed','skipped') THEN now() ELSE finished_at END
		WHERE operation_id=$1 AND idx=$2`, opID, idx, status, jsonOrEmpty(detail), errMsg)
	return err
}

// OperationsInStatus returns operations with the given statuses, oldest first.
func (s *Store) OperationsInStatus(ctx context.Context, statuses ...string) ([]*Operation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+opColumns+` FROM operations WHERE status = ANY($1) ORDER BY created_at`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Operation
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ActiveOperation returns the pending, running or paused operation of a cluster, if any.
func (s *Store) ActiveOperation(ctx context.Context, clusterID string) (*Operation, error) {
	return scanOperation(s.Pool.QueryRow(ctx, `SELECT `+opColumns+` FROM operations
		WHERE cluster_id=$1 AND status IN ('pending','running','paused')`, clusterID))
}

func jsonOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return raw
}
