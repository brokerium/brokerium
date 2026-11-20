// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// Setting is a setting of the installation: plain values as JSON and sealed credentials.
type Setting struct {
	Key           string
	Value         json.RawMessage
	SecretsSealed []byte
	UpdatedAt     time.Time
	UpdatedBy     string
}

// GetSetting returns a setting of the installation, or ErrNotFound.
func (s *Store) GetSetting(ctx context.Context, key string) (*Setting, error) {
	st := Setting{Key: key}
	err := s.Pool.QueryRow(ctx, `SELECT value, secrets, updated_at, updated_by FROM installation_settings WHERE key=$1`, key).
		Scan(&st.Value, &st.SecretsSealed, &st.UpdatedAt, &st.UpdatedBy)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// SetSetting stores a setting of the installation, replacing the previous value.
func (s *Store) SetSetting(ctx context.Context, st *Setting) error {
	return s.Pool.QueryRow(ctx, `INSERT INTO installation_settings (key, value, secrets, updated_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, secrets=EXCLUDED.secrets, updated_by=EXCLUDED.updated_by,
		updated_at=now() RETURNING updated_at`, st.Key, jsonOrEmpty(st.Value), st.SecretsSealed, st.UpdatedBy).Scan(&st.UpdatedAt)
}

// DeleteSetting removes a setting of the installation; removing a missing one is no error.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM installation_settings WHERE key=$1`, key)
	return err
}

// ClusterMonitoring tells how the metrics of a cluster are found in Prometheus. An empty
// Selector and a zero ExporterPort mean the defaults.
type ClusterMonitoring struct {
	ClusterID    string
	Selector     string
	ExporterPort int
	UpdatedAt    time.Time
	UpdatedBy    string
}

// GetClusterMonitoring returns the monitoring settings of a cluster, or ErrNotFound when it has
// the defaults.
func (s *Store) GetClusterMonitoring(ctx context.Context, clusterID string) (*ClusterMonitoring, error) {
	m := ClusterMonitoring{ClusterID: clusterID}
	err := s.Pool.QueryRow(ctx, `SELECT selector, exporter_port, updated_at, updated_by FROM cluster_monitoring
		WHERE cluster_id=$1`, clusterID).Scan(&m.Selector, &m.ExporterPort, &m.UpdatedAt, &m.UpdatedBy)
	if err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

// SetClusterMonitoring stores the monitoring settings of a cluster; ErrNotFound when the cluster
// does not exist.
func (s *Store) SetClusterMonitoring(ctx context.Context, m *ClusterMonitoring) error {
	err := s.Pool.QueryRow(ctx, `INSERT INTO cluster_monitoring (cluster_id, selector, exporter_port, updated_by)
		SELECT id, $2, $3, $4 FROM clusters WHERE id=$1
		ON CONFLICT (cluster_id) DO UPDATE SET selector=EXCLUDED.selector, exporter_port=EXCLUDED.exporter_port,
		updated_by=EXCLUDED.updated_by, updated_at=now() RETURNING updated_at`,
		m.ClusterID, m.Selector, m.ExporterPort, m.UpdatedBy).Scan(&m.UpdatedAt)
	return notFound(err)
}
