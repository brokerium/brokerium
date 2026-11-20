// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"time"
)

// ProblemState is the stored state of a finding of the problem rules (ADR 0023): when it was
// first and last seen, who acknowledged it and until when it is muted.
type ProblemState struct {
	ClusterID      string     `json:"clusterId"`
	Fingerprint    string     `json:"fingerprint"`
	Code           string     `json:"code"`
	Object         string     `json:"object"`
	FirstSeen      time.Time  `json:"firstSeen"`
	LastSeen       time.Time  `json:"lastSeen"`
	AcknowledgedBy string     `json:"acknowledgedBy"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	MutedUntil     *time.Time `json:"mutedUntil"`
}

// ProblemKey identifies a finding: the fingerprint of its code and object, and both of them.
type ProblemKey struct {
	Fingerprint string
	Code        string
	Object      string
}

const problemColumns = `cluster_id, fingerprint, code, object, first_seen, last_seen, acknowledged_by, acknowledged_at, muted_until`

func scanProblemState(row scanner) (*ProblemState, error) {
	var p ProblemState
	if err := row.Scan(&p.ClusterID, &p.Fingerprint, &p.Code, &p.Object, &p.FirstSeen, &p.LastSeen, &p.AcknowledgedBy,
		&p.AcknowledgedAt, &p.MutedUntil); err != nil {
		return nil, notFound(err)
	}
	return &p, nil
}

// SeeProblems records that findings of a cluster were seen at a time: findings without a state
// get one, the others a later last-seen time.
func (s *Store) SeeProblems(ctx context.Context, clusterID string, seen []ProblemKey, at time.Time) error {
	if len(seen) == 0 {
		return nil
	}
	fingerprints := make([]string, len(seen))
	codes := make([]string, len(seen))
	objects := make([]string, len(seen))
	for i, k := range seen {
		fingerprints[i], codes[i], objects[i] = k.Fingerprint, k.Code, k.Object
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO problem_states (cluster_id, fingerprint, code, object, first_seen, last_seen)
		SELECT $1, f, c, o, $5, $5 FROM unnest($2::text[], $3::text[], $4::text[]) AS seen (f, c, o)
		ON CONFLICT (cluster_id, fingerprint) DO UPDATE SET last_seen = GREATEST(problem_states.last_seen, EXCLUDED.last_seen)`,
		clusterID, fingerprints, codes, objects, at)
	return err
}

// ProblemStates returns the states of the findings of a cluster, or of every cluster when
// clusterID is empty.
func (s *Store) ProblemStates(ctx context.Context, clusterID string) ([]*ProblemState, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+problemColumns+` FROM problem_states WHERE ($1 = '' OR cluster_id = $1)
		ORDER BY cluster_id, fingerprint`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProblemState
	for rows.Next() {
		p, err := scanProblemState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AcknowledgeProblem records that actor acknowledged a finding at a time. A finding without a
// state gets one.
func (s *Store) AcknowledgeProblem(ctx context.Context, clusterID string, k ProblemKey, actor string, at time.Time) (*ProblemState, error) {
	return scanProblemState(s.Pool.QueryRow(ctx, `INSERT INTO problem_states (cluster_id, fingerprint, code, object, first_seen,
		last_seen, acknowledged_by, acknowledged_at) VALUES ($1, $2, $3, $4, $6, $6, $5, $6)
		ON CONFLICT (cluster_id, fingerprint) DO UPDATE SET acknowledged_by = EXCLUDED.acknowledged_by,
		acknowledged_at = EXCLUDED.acknowledged_at RETURNING `+problemColumns,
		clusterID, k.Fingerprint, k.Code, k.Object, actor, at))
}

// UnacknowledgeProblem removes the acknowledgement of a finding.
func (s *Store) UnacknowledgeProblem(ctx context.Context, clusterID, fingerprint string) (*ProblemState, error) {
	return scanProblemState(s.Pool.QueryRow(ctx, `UPDATE problem_states SET acknowledged_by = '', acknowledged_at = NULL
		WHERE cluster_id = $1 AND fingerprint = $2 RETURNING `+problemColumns, clusterID, fingerprint))
}

// MuteProblem hides a finding until a time. A finding without a state gets one, seen at now.
func (s *Store) MuteProblem(ctx context.Context, clusterID string, k ProblemKey, until, now time.Time) (*ProblemState, error) {
	return scanProblemState(s.Pool.QueryRow(ctx, `INSERT INTO problem_states (cluster_id, fingerprint, code, object, first_seen,
		last_seen, muted_until) VALUES ($1, $2, $3, $4, $6, $6, $5)
		ON CONFLICT (cluster_id, fingerprint) DO UPDATE SET muted_until = EXCLUDED.muted_until RETURNING `+problemColumns,
		clusterID, k.Fingerprint, k.Code, k.Object, until, now))
}

// UnmuteProblem shows a muted finding again.
func (s *Store) UnmuteProblem(ctx context.Context, clusterID, fingerprint string) (*ProblemState, error) {
	return scanProblemState(s.Pool.QueryRow(ctx, `UPDATE problem_states SET muted_until = NULL
		WHERE cluster_id = $1 AND fingerprint = $2 RETURNING `+problemColumns, clusterID, fingerprint))
}

// ForgetProblems deletes the states of findings that were last seen before a time and returns
// how many it deleted.
func (s *Store) ForgetProblems(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM problem_states WHERE last_seen < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
