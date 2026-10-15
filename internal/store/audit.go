// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// AuditEntry records who changed what.
type AuditEntry struct {
	ID        int64           `json:"id"`
	At        time.Time       `json:"at"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	ClusterID string          `json:"clusterId"`
	Target    string          `json:"target"`
	Result    string          `json:"result"`
	Detail    json.RawMessage `json:"detail"`
}

// AddAudit appends an audit entry.
func (s *Store) AddAudit(ctx context.Context, e AuditEntry) error {
	if e.Result == "" {
		e.Result = "ok"
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO audit_log (actor, action, cluster_id, target, result, detail)
		VALUES ($1,$2,$3,$4,$5,$6)`, e.Actor, e.Action, e.ClusterID, e.Target, e.Result, jsonOrEmpty(e.Detail))
	return err
}

// ListAudit returns recent audit entries, optionally for one cluster.
func (s *Store) ListAudit(ctx context.Context, clusterID string, limit int) ([]*AuditEntry, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, at, actor, action, cluster_id, target, result, detail FROM audit_log
		WHERE ($1 = '' OR cluster_id = $1) ORDER BY at DESC LIMIT $2`, clusterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.ClusterID, &e.Target, &e.Result, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
