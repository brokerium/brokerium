// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// Node is a host running brokerium-agent.
type Node struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	ClusterID       *string `json:"clusterId"`
	Role            string  `json:"role"`
	KafkaNodeID     *int32  `json:"kafkaNodeId"`
	Hostname        string  `json:"hostname"`
	AgentVersion    string  `json:"agentVersion"`
	CertFingerprint string  `json:"-"`
	// PrevCertFingerprint is the certificate a renewal was requested with; it stays valid until
	// the agent connects with the renewed certificate.
	PrevCertFingerprint string            `json:"-"`
	CertNotAfter        *time.Time        `json:"certNotAfter"`
	Status              json.RawMessage   `json:"status"`
	Labels              map[string]string `json:"labels"`
	ProviderRef         string            `json:"providerRef"`
	LastSeenAt          *time.Time        `json:"lastSeenAt"`
	CreatedAt           time.Time         `json:"createdAt"`
}

const nodeColumns = `id, name, cluster_id, role, kafka_node_id, hostname, agent_version, cert_fingerprint,
	prev_cert_fingerprint, cert_not_after, status, labels, provider_ref, last_seen_at, created_at`

func scanNode(row scanner) (*Node, error) {
	var n Node
	var labels []byte
	if err := row.Scan(&n.ID, &n.Name, &n.ClusterID, &n.Role, &n.KafkaNodeID, &n.Hostname, &n.AgentVersion,
		&n.CertFingerprint, &n.PrevCertFingerprint, &n.CertNotAfter, &n.Status, &labels, &n.ProviderRef,
		&n.LastSeenAt, &n.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(labels, &n.Labels)
	return &n, nil
}

// CreateNode inserts a node record.
func (s *Store) CreateNode(ctx context.Context, n *Node) error {
	if n.ID == "" {
		n.ID = NewID()
	}
	labels, _ := json.Marshal(nonNilMap(n.Labels))
	err := s.Pool.QueryRow(ctx, `INSERT INTO nodes (id, name, cluster_id, role, kafka_node_id, hostname, agent_version,
		cert_fingerprint, status, labels, provider_ref) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING created_at`,
		n.ID, n.Name, n.ClusterID, n.Role, n.KafkaNodeID, n.Hostname, n.AgentVersion, n.CertFingerprint,
		jsonOrEmpty(n.Status), labels, n.ProviderRef).Scan(&n.CreatedAt)
	return conflict(err)
}

// GetNode returns a node by id.
func (s *Store) GetNode(ctx context.Context, id string) (*Node, error) {
	return scanNode(s.Pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id=$1`, id))
}

// GetNodeByName returns a node by name.
func (s *Store) GetNodeByName(ctx context.Context, name string) (*Node, error) {
	return scanNode(s.Pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE name=$1`, name))
}

// ListNodes returns nodes, optionally of one cluster.
func (s *Store) ListNodes(ctx context.Context, clusterID string) ([]*Node, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE ($1 = '' OR cluster_id = $1) ORDER BY name`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateNodeEnrollment stores the certificate fingerprint and agent details after enrollment.
func (s *Store) UpdateNodeEnrollment(ctx context.Context, id, fingerprint string, notAfter time.Time, hostname, agentVersion string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET cert_fingerprint=$2, cert_not_after=$3, prev_cert_fingerprint='',
		hostname=$4, agent_version=$5, last_seen_at=now() WHERE id=$1`, id, fingerprint, notAfter, hostname, agentVersion)
	return err
}

// RenewNodeCertificate makes a renewed certificate current and keeps the certificate the renewal
// was requested with valid.
func (s *Store) RenewNodeCertificate(ctx context.Context, id, previous, current string, notAfter time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET prev_cert_fingerprint=$2, cert_fingerprint=$3, cert_not_after=$4
		WHERE id=$1`, id, previous, current, notAfter)
	return err
}

// ConfirmNodeCertificate records that the agent connected with its current certificate: the
// previous one is no longer accepted.
func (s *Store) ConfirmNodeCertificate(ctx context.Context, id, fingerprint string, notAfter time.Time) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET prev_cert_fingerprint='', cert_not_after=$3
		WHERE id=$1 AND cert_fingerprint=$2`, id, fingerprint, notAfter)
	return err
}

// UpdateNodeStatus stores the latest status reported by the agent.
func (s *Store) UpdateNodeStatus(ctx context.Context, id string, status json.RawMessage) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET status=$2, last_seen_at=now() WHERE id=$1`, id, jsonOrEmpty(status))
	return err
}

// AssignNode binds a node to a cluster with a role and Kafka node id.
func (s *Store) AssignNode(ctx context.Context, id string, clusterID *string, role string, kafkaNodeID *int32) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET cluster_id=$2, role=$3, kafka_node_id=$4 WHERE id=$1`,
		id, clusterID, role, kafkaNodeID)
	return err
}

// DeleteNode removes a node record.
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM nodes WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// JoinToken authorizes one agent enrollment.
type JoinToken struct {
	TokenHash string     `json:"-"`
	NodeName  string     `json:"nodeName"`
	ClusterID *string    `json:"clusterId"`
	CreatedBy string     `json:"createdBy"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// CreateJoinToken stores a hashed join token.
func (s *Store) CreateJoinToken(ctx context.Context, t *JoinToken) error {
	return s.Pool.QueryRow(ctx, `INSERT INTO join_tokens (token_hash, node_name, cluster_id, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING created_at`, t.TokenHash, t.NodeName, t.ClusterID, t.CreatedBy, t.ExpiresAt).
		Scan(&t.CreatedAt)
}

// ConsumeJoinToken marks an unused, unexpired token as used and returns it.
func (s *Store) ConsumeJoinToken(ctx context.Context, tokenHash string) (*JoinToken, error) {
	var t JoinToken
	err := s.Pool.QueryRow(ctx, `UPDATE join_tokens SET used_at=now()
		WHERE token_hash=$1 AND used_at IS NULL AND expires_at > now()
		RETURNING token_hash, node_name, cluster_id, created_by, expires_at, used_at, created_at`, tokenHash).
		Scan(&t.TokenHash, &t.NodeName, &t.ClusterID, &t.CreatedBy, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

// GetCA returns the stored certificate authority.
func (s *Store) GetCA(ctx context.Context) (certPEM string, keySealed []byte, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT cert_pem, key_ciphertext FROM certificate_authority WHERE id=1`).Scan(&certPEM, &keySealed)
	return certPEM, keySealed, notFound(err)
}

// SaveCA stores the certificate authority once.
func (s *Store) SaveCA(ctx context.Context, certPEM string, keySealed []byte) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO certificate_authority (id, cert_pem, key_ciphertext) VALUES (1,$1,$2)
		ON CONFLICT (id) DO NOTHING`, certPEM, keySealed)
	return err
}
