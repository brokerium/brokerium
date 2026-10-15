// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// Providers define how the nodes of a cluster are managed (ADR 0004).
const (
	ProviderExternal = "external"
	ProviderAgent    = "agent"
	ProviderDocker   = "docker"
	ProviderStrimzi  = "strimzi"
)

// Flavors describe the Kafka distribution or managed service.
const (
	FlavorApache         = "apache"
	FlavorMSK            = "msk"
	FlavorConfluentCloud = "confluent-cloud"
	FlavorAiven          = "aiven"
)

// Security holds non-secret connection settings.
type Security struct {
	Protocol              string `json:"protocol"`
	SASLMechanism         string `json:"saslMechanism,omitempty"`
	Username              string `json:"username,omitempty"`
	TLSServerName         string `json:"tlsServerName,omitempty"`
	TLSInsecureSkipVerify bool   `json:"tlsInsecureSkipVerify,omitempty"`
	TLSCACertPEM          string `json:"tlsCaCertPem,omitempty"`
	AWSRegion             string `json:"awsRegion,omitempty"`
}

// Secrets holds credentials; they are stored encrypted and never returned by the API.
type Secrets struct {
	Password           string `json:"password,omitempty"`
	TLSClientCertPEM   string `json:"tlsClientCertPem,omitempty"`
	TLSClientKeyPEM    string `json:"tlsClientKeyPem,omitempty"`
	AWSAccessKeyID     string `json:"awsAccessKeyId,omitempty"`
	AWSSecretAccessKey string `json:"awsSecretAccessKey,omitempty"`
	AWSSessionToken    string `json:"awsSessionToken,omitempty"`
	OAuthToken         string `json:"oauthToken,omitempty"`
}

// Cluster is a registered Kafka cluster.
type Cluster struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Provider         string            `json:"provider"`
	Flavor           string            `json:"flavor"`
	BootstrapServers []string          `json:"bootstrapServers"`
	Security         Security          `json:"security"`
	SecretsSealed    []byte            `json:"-"`
	Labels           map[string]string `json:"labels"`
	Settings         map[string]any    `json:"settings"`
	KafkaClusterID   string            `json:"kafkaClusterId"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

const clusterColumns = `id, name, provider, flavor, bootstrap_servers, security, secrets, labels, settings,
	kafka_cluster_id, created_at, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanCluster(row scanner) (*Cluster, error) {
	var c Cluster
	var security, labels, settings []byte
	if err := row.Scan(&c.ID, &c.Name, &c.Provider, &c.Flavor, &c.BootstrapServers, &security, &c.SecretsSealed,
		&labels, &settings, &c.KafkaClusterID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(security, &c.Security)
	_ = json.Unmarshal(labels, &c.Labels)
	_ = json.Unmarshal(settings, &c.Settings)
	if c.Labels == nil {
		c.Labels = map[string]string{}
	}
	if c.Settings == nil {
		c.Settings = map[string]any{}
	}
	return &c, nil
}

// ListClusters returns all clusters ordered by name.
func (s *Store) ListClusters(ctx context.Context) ([]*Cluster, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+clusterColumns+` FROM clusters ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Cluster
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCluster returns one cluster.
func (s *Store) GetCluster(ctx context.Context, id string) (*Cluster, error) {
	return scanCluster(s.Pool.QueryRow(ctx, `SELECT `+clusterColumns+` FROM clusters WHERE id = $1`, id))
}

// CreateCluster inserts a cluster; ID and timestamps are set here.
func (s *Store) CreateCluster(ctx context.Context, c *Cluster) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	security, _ := json.Marshal(c.Security)
	labels, _ := json.Marshal(nonNilMap(c.Labels))
	settings, _ := json.Marshal(nonNilAnyMap(c.Settings))
	err := s.Pool.QueryRow(ctx, `INSERT INTO clusters (id, name, provider, flavor, bootstrap_servers, security, secrets,
		labels, settings, kafka_cluster_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at, updated_at`,
		c.ID, c.Name, c.Provider, c.Flavor, c.BootstrapServers, security, c.SecretsSealed, labels, settings,
		c.KafkaClusterID).Scan(&c.CreatedAt, &c.UpdatedAt)
	return conflict(err)
}

// UpdateCluster overwrites the mutable fields of a cluster.
func (s *Store) UpdateCluster(ctx context.Context, c *Cluster) error {
	security, _ := json.Marshal(c.Security)
	labels, _ := json.Marshal(nonNilMap(c.Labels))
	settings, _ := json.Marshal(nonNilAnyMap(c.Settings))
	tag, err := s.Pool.Exec(ctx, `UPDATE clusters SET name=$2, provider=$3, flavor=$4, bootstrap_servers=$5, security=$6,
		secrets=$7, labels=$8, settings=$9, kafka_cluster_id=$10, updated_at=now() WHERE id=$1`,
		c.ID, c.Name, c.Provider, c.Flavor, c.BootstrapServers, security, c.SecretsSealed, labels, settings, c.KafkaClusterID)
	if err != nil {
		return conflict(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetKafkaClusterID records the cluster id reported by Kafka.
func (s *Store) SetKafkaClusterID(ctx context.Context, id, kafkaClusterID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE clusters SET kafka_cluster_id=$2 WHERE id=$1 AND kafka_cluster_id <> $2`, id, kafkaClusterID)
	return err
}

// DeleteCluster removes a cluster and its operations.
func (s *Store) DeleteCluster(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM clusters WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
