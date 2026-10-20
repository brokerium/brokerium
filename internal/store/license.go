// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"time"
)

// LicenseKey is the license key stored through the dashboard.
type LicenseKey struct {
	Key       string
	UpdatedAt time.Time
	UpdatedBy string
}

// GetLicenseKey returns the stored license key, or ErrNotFound.
func (s *Store) GetLicenseKey(ctx context.Context) (*LicenseKey, error) {
	var k LicenseKey
	err := s.Pool.QueryRow(ctx, `SELECT key, updated_at, updated_by FROM license WHERE id=1`).Scan(&k.Key, &k.UpdatedAt, &k.UpdatedBy)
	if err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

// SetLicenseKey stores the license key, replacing the previous one.
func (s *Store) SetLicenseKey(ctx context.Context, key, actor string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO license (id, key, updated_by) VALUES (1,$1,$2)
		ON CONFLICT (id) DO UPDATE SET key=EXCLUDED.key, updated_by=EXCLUDED.updated_by, updated_at=now()`, key, actor)
	return err
}

// DeleteLicenseKey removes the stored license key.
func (s *Store) DeleteLicenseKey(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM license WHERE id=1`)
	return err
}
