// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// Transports an agent enrolls with (ADR 0017).
const (
	TransportMTLS     = "mtls"
	TransportInsecure = "insecure"
)

// Node is a host running brokerium-agent.
type Node struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	ClusterID   *string `json:"clusterId"`
	Role        string  `json:"role"`
	KafkaNodeID *int32  `json:"kafkaNodeId"`
	Hostname    string  `json:"hostname"`
	// AdvertisedHost is the address the listeners of the node advertise and the quorum and the
	// server use; it starts as the host name the agent reported (ADR 0018).
	AdvertisedHost string `json:"advertisedHost"`
	// Rack is the broker.rack the node gets when it joins a cluster; empty for none.
	Rack string `json:"rack"`
	// KafkaStopPending is set when the node left its cluster while its agent was not connected:
	// Kafka is stopped when the agent connects again.
	KafkaStopPending bool   `json:"-"`
	AgentVersion     string `json:"agentVersion"`
	// Transport of the last enrollment: mtls (client certificate) or insecure (node secret).
	Transport       string `json:"transport"`
	CertFingerprint string `json:"-"`
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

const nodeColumns = `id, name, cluster_id, role, kafka_node_id, hostname, advertised_host, rack, kafka_stop_pending,
	agent_version, transport, cert_fingerprint, prev_cert_fingerprint, cert_not_after, status, labels, provider_ref,
	last_seen_at, created_at`

func scanNode(row scanner) (*Node, error) {
	var n Node
	var labels []byte
	if err := row.Scan(&n.ID, &n.Name, &n.ClusterID, &n.Role, &n.KafkaNodeID, &n.Hostname, &n.AdvertisedHost, &n.Rack,
		&n.KafkaStopPending, &n.AgentVersion, &n.Transport, &n.CertFingerprint, &n.PrevCertFingerprint, &n.CertNotAfter,
		&n.Status, &labels, &n.ProviderRef, &n.LastSeenAt, &n.CreatedAt); err != nil {
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
	err := s.Pool.QueryRow(ctx, `INSERT INTO nodes (id, name, cluster_id, role, kafka_node_id, hostname, advertised_host,
		rack, agent_version, cert_fingerprint, status, labels, provider_ref) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING created_at`,
		n.ID, n.Name, n.ClusterID, n.Role, n.KafkaNodeID, n.Hostname, n.AdvertisedHost, n.Rack, n.AgentVersion,
		n.CertFingerprint, jsonOrEmpty(n.Status), labels, n.ProviderRef).Scan(&n.CreatedAt)
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

// UpdateNodeEnrollment stores the certificate fingerprint and agent details after an
// enrollment with mutual TLS. The node secret of an earlier insecure enrollment is dropped. The
// reported host name becomes the advertised host of a node that has none.
func (s *Store) UpdateNodeEnrollment(ctx context.Context, id, fingerprint string, notAfter time.Time, hostname, agentVersion string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET transport='mtls', secret_ciphertext=NULL, cert_fingerprint=$2,
		cert_not_after=$3, prev_cert_fingerprint='', hostname=$4, agent_version=$5,
		advertised_host=COALESCE(NULLIF(advertised_host, ''), $4), last_seen_at=now() WHERE id=$1`,
		id, fingerprint, notAfter, hostname, agentVersion)
	return err
}

// UpdateNodeInsecureEnrollment stores the sealed node secret and agent details after an
// enrollment with the insecure transport. The certificates of earlier enrollments no longer
// authenticate. The reported host name becomes the advertised host of a node that has none.
func (s *Store) UpdateNodeInsecureEnrollment(ctx context.Context, id string, sealedSecret []byte, hostname, agentVersion string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET transport='insecure', secret_ciphertext=$2, cert_fingerprint='',
		prev_cert_fingerprint='', cert_not_after=NULL, hostname=$3, agent_version=$4,
		advertised_host=COALESCE(NULLIF(advertised_host, ''), $3), last_seen_at=now() WHERE id=$1`,
		id, sealedSecret, hostname, agentVersion)
	return err
}

// UpdateNodeDetails stores the name, advertised host and rack of a node.
func (s *Store) UpdateNodeDetails(ctx context.Context, n *Node) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE nodes SET name=$2, advertised_host=$3, rack=$4 WHERE id=$1`,
		n.ID, n.Name, n.AdvertisedHost, n.Rack)
	if err != nil {
		return conflict(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// NodeSecret returns the sealed secret of a node enrolled with the insecure transport.
func (s *Store) NodeSecret(ctx context.Context, id string) ([]byte, error) {
	var sealed []byte
	err := s.Pool.QueryRow(ctx, `SELECT secret_ciphertext FROM nodes WHERE id=$1 AND transport='insecure'
		AND secret_ciphertext IS NOT NULL`, id).Scan(&sealed)
	return sealed, notFound(err)
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

// ClaimNode gives a node a Kafka role and node id in a cluster. It fails with ErrConflict when
// the node has another role or belongs to another cluster, or when another node of the cluster
// has that Kafka node id; claiming the assignment a node already has succeeds. A pending stop of
// Kafka is dropped: the node runs Kafka for the cluster now.
func (s *Store) ClaimNode(ctx context.Context, id, clusterID, role string, kafkaNodeID int32) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE nodes SET cluster_id=$2, role=$3, kafka_node_id=$4, kafka_stop_pending=false WHERE id=$1
		AND (cluster_id IS NULL OR cluster_id=$2) AND (role='' OR (role=$3 AND kafka_node_id=$4))
		AND NOT EXISTS (SELECT 1 FROM nodes o WHERE o.cluster_id=$2 AND o.kafka_node_id=$4 AND o.id<>$1)`,
		id, clusterID, role, kafkaNodeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetNode(ctx, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// ReleaseNode removes the Kafka role and the cluster of a node that runs Kafka node kafkaNodeID
// of the cluster; a node with another assignment stays as it is. With stopPending, Kafka still
// has to be stopped on the node when its agent connects.
func (s *Store) ReleaseNode(ctx context.Context, id, clusterID string, kafkaNodeID int32, stopPending bool) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET cluster_id=NULL, role='', kafka_node_id=NULL, kafka_stop_pending=$4
		WHERE id=$1 AND cluster_id=$2 AND kafka_node_id=$3`, id, clusterID, kafkaNodeID, stopPending)
	return err
}

// KafkaStopped clears the pending stop of Kafka on a node.
func (s *Store) KafkaStopped(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE nodes SET kafka_stop_pending=false WHERE id=$1`, id)
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

// JoinToken authorizes one agent enrollment. It names the node it creates, leaves the name to
// the host name the agent reports (empty NodeName) or enrolls the node NodeID again.
type JoinToken struct {
	TokenHash string     `json:"-"`
	NodeName  string     `json:"nodeName"`
	NodeID    *string    `json:"nodeId"`
	ClusterID *string    `json:"clusterId"`
	CreatedBy string     `json:"createdBy"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

const joinColumns = `token_hash, node_name, node_id, cluster_id, created_by, expires_at, used_at, created_at`

func scanJoinToken(row scanner) (*JoinToken, error) {
	var t JoinToken
	if err := row.Scan(&t.TokenHash, &t.NodeName, &t.NodeID, &t.ClusterID, &t.CreatedBy, &t.ExpiresAt, &t.UsedAt,
		&t.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &t, nil
}

// CreateJoinToken stores a hashed join token.
func (s *Store) CreateJoinToken(ctx context.Context, t *JoinToken) error {
	return s.Pool.QueryRow(ctx, `INSERT INTO join_tokens (token_hash, node_name, node_id, cluster_id, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at`, t.TokenHash, t.NodeName, t.NodeID, t.ClusterID, t.CreatedBy,
		t.ExpiresAt).Scan(&t.CreatedAt)
}

// FindJoinToken returns the unused, unexpired token whose hash has the given SHA-256, the
// lookup of an enrollment proof.
func (s *Store) FindJoinToken(ctx context.Context, lookup []byte) (*JoinToken, error) {
	return scanJoinToken(s.Pool.QueryRow(ctx, `SELECT `+joinColumns+` FROM join_tokens
		WHERE sha256(convert_to(token_hash, 'UTF8')) = $1 AND used_at IS NULL AND expires_at > now()`, lookup))
}

// ConsumeJoinToken marks an unused, unexpired token as used and returns it.
func (s *Store) ConsumeJoinToken(ctx context.Context, tokenHash string) (*JoinToken, error) {
	return scanJoinToken(s.Pool.QueryRow(ctx, `UPDATE join_tokens SET used_at=now()
		WHERE token_hash=$1 AND used_at IS NULL AND expires_at > now() RETURNING `+joinColumns, tokenHash))
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
