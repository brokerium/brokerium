// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/bvaliev/brokerium/internal/cluster"
	"github.com/bvaliev/brokerium/internal/kafka"
	"github.com/bvaliev/brokerium/internal/store"
)

var clusterNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func (s *Server) registerClusters() {
	s.Route("GET /api/v1/overview", store.RoleViewer, s.handleOverview)
	s.Route("GET /api/v1/clusters", store.RoleViewer, s.handleOverview)
	s.Route("POST /api/v1/clusters", store.RoleOperator, s.handleCreateCluster)
	s.Route("POST /api/v1/clusters/test", store.RoleOperator, s.handleTestCluster)
	s.Route("GET /api/v1/clusters/{id}", store.RoleViewer, s.handleGetCluster)
	s.Route("PATCH /api/v1/clusters/{id}", store.RoleOperator, s.handleUpdateCluster)
	s.Route("DELETE /api/v1/clusters/{id}", store.RoleAdmin, s.handleDeleteCluster)
	s.Route("POST /api/v1/clusters/{id}/refresh", store.RoleViewer, s.handleRefreshCluster)
	s.Route("GET /api/v1/clusters/{id}/topics/{topic}", store.RoleViewer, s.handleTopicDetail)
	s.Route("GET /api/v1/clusters/{id}/brokers/{broker}/configs", store.RoleViewer, s.handleBrokerConfigs)
	s.Route("GET /api/v1/clusters/{id}/groups/{group}", store.RoleViewer, s.handleGroupDetail)
}

// ClusterSummary is one row of the multi-cluster overview.
type ClusterSummary struct {
	Cluster      *store.Cluster       `json:"cluster"`
	Health       cluster.Health       `json:"health"`
	Capabilities cluster.Capabilities `json:"capabilities"`
	Error        string               `json:"error,omitempty"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	views, err := s.Clusters.Views(r.Context(), time.Minute)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	out := make([]ClusterSummary, 0, len(views))
	for _, v := range views {
		out = append(out, ClusterSummary{Cluster: v.Cluster, Health: v.Health, Capabilities: v.Capabilities, Error: v.Error, UpdatedAt: v.UpdatedAt})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"clusters": out})
}

// ClusterRequest creates or updates a cluster registration.
type ClusterRequest struct {
	Name             *string           `json:"name"`
	Provider         *string           `json:"provider"`
	Flavor           *string           `json:"flavor"`
	BootstrapServers []string          `json:"bootstrapServers"`
	Security         *store.Security   `json:"security"`
	Secrets          *store.Secrets    `json:"secrets"`
	Labels           map[string]string `json:"labels"`
	Settings         map[string]any    `json:"settings"`
	SkipTest         bool              `json:"skipTest"`
}

func (req *ClusterRequest) apply(c *store.Cluster) error {
	if req.Name != nil {
		c.Name = strings.TrimSpace(*req.Name)
	}
	if req.Provider != nil {
		c.Provider = *req.Provider
	}
	if req.Flavor != nil {
		c.Flavor = *req.Flavor
	}
	if req.BootstrapServers != nil {
		c.BootstrapServers = nil
		for _, b := range req.BootstrapServers {
			if b = strings.TrimSpace(b); b != "" {
				c.BootstrapServers = append(c.BootstrapServers, b)
			}
		}
	}
	if req.Security != nil {
		c.Security = *req.Security
	}
	if req.Labels != nil {
		c.Labels = req.Labels
	}
	if req.Settings != nil {
		c.Settings = req.Settings
	}
	if c.Provider == "" {
		c.Provider = store.ProviderExternal
	}
	if c.Flavor == "" {
		c.Flavor = store.FlavorApache
	}
	if c.Security.Protocol == "" {
		c.Security.Protocol = kafka.ProtocolPlaintext
	}
	c.Security.Protocol = strings.ToUpper(c.Security.Protocol)
	switch {
	case !clusterNameRe.MatchString(c.Name):
		return fmt.Errorf("name must be lowercase letters, digits and dashes (max 63)")
	case len(c.BootstrapServers) == 0:
		return fmt.Errorf("at least one bootstrap server is required")
	}
	switch c.Provider {
	case store.ProviderExternal, store.ProviderAgent, store.ProviderDocker, store.ProviderStrimzi:
	default:
		return fmt.Errorf("unknown provider %q", c.Provider)
	}
	switch c.Flavor {
	case store.FlavorApache, store.FlavorMSK, store.FlavorConfluentCloud, store.FlavorAiven:
	default:
		return fmt.Errorf("unknown flavor %q", c.Flavor)
	}
	switch c.Security.Protocol {
	case kafka.ProtocolPlaintext, kafka.ProtocolSSL:
	case kafka.ProtocolSASLPlaintext, kafka.ProtocolSASLSSL:
		if c.Security.SASLMechanism == "" {
			return fmt.Errorf("saslMechanism is required for %s", c.Security.Protocol)
		}
	default:
		return fmt.Errorf("unknown security protocol %q", c.Security.Protocol)
	}
	return nil
}

func (s *Server) sealSecrets(c *store.Cluster, secrets *store.Secrets) error {
	if secrets == nil {
		return nil
	}
	sealed, err := s.Box.SealJSON(secrets)
	if err != nil {
		return err
	}
	c.SecretsSealed = sealed
	return nil
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var req ClusterRequest
	if !Decode(w, r, &req) {
		return
	}
	c := &store.Cluster{}
	if err := req.apply(c); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.sealSecrets(c, req.Secrets); err != nil {
		WriteStoreError(w, err)
		return
	}
	if !req.SkipTest {
		p, err := s.Clusters.Profile(c)
		if err != nil {
			WriteStoreError(w, err)
			return
		}
		res := cluster.TestConnection(r.Context(), p)
		if !res.OK {
			WriteError(w, http.StatusBadRequest, "connection_failed", res.Error)
			return
		}
		c.KafkaClusterID = res.ClusterID
		if existing, err := s.Store.ListClusters(r.Context()); err == nil {
			for _, e := range existing {
				if e.KafkaClusterID != "" && e.KafkaClusterID == res.ClusterID {
					WriteError(w, http.StatusConflict, "duplicate_cluster",
						fmt.Sprintf("Kafka cluster %s is already registered as %q", res.ClusterID, e.Name))
					return
				}
			}
		}
	}
	err := s.Store.CreateCluster(r.Context(), c)
	s.Audit(r, "cluster.create", c.ID, c.Name, map[string]any{"provider": c.Provider, "flavor": c.Flavor,
		"bootstrapServers": c.BootstrapServers}, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	v, _ := s.Clusters.Refresh(r.Context(), c.ID)
	WriteJSON(w, http.StatusCreated, v)
}

func (s *Server) handleTestCluster(w http.ResponseWriter, r *http.Request) {
	var req ClusterRequest
	if !Decode(w, r, &req) {
		return
	}
	c := &store.Cluster{}
	if req.Name == nil {
		name := "test"
		req.Name = &name
	}
	if err := req.apply(c); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p := kafka.Profile{BootstrapServers: c.BootstrapServers, Security: c.Security}
	if req.Secrets != nil {
		p.Secrets = *req.Secrets
	}
	WriteJSON(w, http.StatusOK, cluster.TestConnection(r.Context(), p))
}

func (s *Server) handleGetCluster(w http.ResponseWriter, r *http.Request) {
	maxAge := 20 * time.Second
	if r.URL.Query().Get("fresh") == "1" {
		maxAge = 0
	}
	v, err := s.Clusters.View(r.Context(), r.PathValue("id"), maxAge)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (s *Server) handleRefreshCluster(w http.ResponseWriter, r *http.Request) {
	v, err := s.Clusters.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (s *Server) handleUpdateCluster(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	var req ClusterRequest
	if !Decode(w, r, &req) {
		return
	}
	if err := req.apply(c); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.sealSecrets(c, req.Secrets); err != nil {
		WriteStoreError(w, err)
		return
	}
	err = s.Store.UpdateCluster(r.Context(), c)
	s.Audit(r, "cluster.update", c.ID, c.Name, map[string]any{"bootstrapServers": c.BootstrapServers,
		"credentialsChanged": req.Secrets != nil}, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	s.Clusters.Invalidate(c.ID)
	v, _ := s.Clusters.Refresh(r.Context(), c.ID)
	WriteJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	err = s.Store.DeleteCluster(r.Context(), c.ID)
	s.Audit(r, "cluster.delete", c.ID, c.Name, nil, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	s.Clusters.Invalidate(c.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ConfigEntry is one configuration value with its source.
type ConfigEntry struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	Sensitive bool   `json:"sensitive"`
}

func configEntries(rc kadm.ResourceConfig) []ConfigEntry {
	out := make([]ConfigEntry, 0, len(rc.Configs))
	for _, c := range rc.Configs {
		e := ConfigEntry{Name: c.Key, Source: c.Source.String(), Sensitive: c.Sensitive}
		if c.Value != nil && !c.Sensitive {
			e.Value = *c.Value
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Server) handleTopicDetail(w http.ResponseWriter, r *http.Request) {
	id, topic := r.PathValue("id"), r.PathValue("topic")
	v, err := s.Clusters.View(r.Context(), id, 20*time.Second)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if v.Snapshot == nil {
		WriteError(w, http.StatusBadGateway, "unreachable", v.Error)
		return
	}
	var found *kafka.Topic
	for i := range v.Snapshot.Topics {
		if v.Snapshot.Topics[i].Name == topic {
			found = &v.Snapshot.Topics[i]
		}
	}
	if found == nil {
		WriteError(w, http.StatusNotFound, "not_found", "topic not found")
		return
	}
	cl, _, err := s.Clusters.Client(r.Context(), id)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp := map[string]any{"topic": found, "configs": []ConfigEntry{}}
	if rcs, err := cl.Adm.DescribeTopicConfigs(ctx, topic); err == nil && len(rcs) == 1 {
		resp["configs"] = configEntries(rcs[0])
	}
	start, _ := cl.Adm.ListStartOffsets(ctx, topic)
	end, _ := cl.Adm.ListEndOffsets(ctx, topic)
	offsets := map[int32]map[string]int64{}
	start.Each(func(o kadm.ListedOffset) {
		offsets[o.Partition] = map[string]int64{"start": o.Offset}
	})
	end.Each(func(o kadm.ListedOffset) {
		if offsets[o.Partition] == nil {
			offsets[o.Partition] = map[string]int64{}
		}
		offsets[o.Partition]["end"] = o.Offset
	})
	resp["offsets"] = offsets
	WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) handleBrokerConfigs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	brokerID, err := strconv.Atoi(r.PathValue("broker"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid broker id")
		return
	}
	v, err := s.Clusters.View(r.Context(), id, 20*time.Second)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	alive := false
	if v.Snapshot != nil {
		for _, b := range v.Snapshot.Brokers {
			if b.ID == int32(brokerID) && b.Alive {
				alive = true
			}
		}
	}
	if !alive {
		WriteError(w, http.StatusConflict, "broker_offline", "configs can only be read from a live broker")
		return
	}
	cl, _, err := s.Clusters.Client(r.Context(), id)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rcs, err := cl.Adm.DescribeBrokerConfigs(ctx, int32(brokerID))
	if err != nil || len(rcs) == 0 {
		WriteError(w, http.StatusBadGateway, "kafka_error", fmt.Sprint(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"broker": brokerID, "configs": configEntries(rcs[0])})
}

// GroupPartitionLag is the lag of one partition in a consumer group.
type GroupPartitionLag struct {
	Topic     string `json:"topic"`
	Partition int32  `json:"partition"`
	Committed int64  `json:"committed"`
	End       int64  `json:"end"`
	Lag       int64  `json:"lag"`
	Member    string `json:"member"`
	ClientID  string `json:"clientId"`
	Host      string `json:"host"`
}

func (s *Server) handleGroupDetail(w http.ResponseWriter, r *http.Request) {
	cl, _, err := s.Clusters.Client(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	group := r.PathValue("group")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	lags, err := cl.Adm.Lag(ctx, group)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "kafka_error", err.Error())
		return
	}
	l, ok := lags[group]
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "group not found")
		return
	}
	rows := []GroupPartitionLag{}
	var total int64
	for topic, parts := range l.Lag {
		for p, pl := range parts {
			row := GroupPartitionLag{Topic: topic, Partition: p, Committed: pl.Commit.At, End: pl.End.Offset, Lag: pl.Lag}
			if pl.Member != nil {
				row.Member, row.ClientID, row.Host = pl.Member.MemberID, pl.Member.ClientID, pl.Member.ClientHost
			}
			if pl.Lag > 0 {
				total += pl.Lag
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Topic < rows[j].Topic || (rows[i].Topic == rows[j].Topic && rows[i].Partition < rows[j].Partition)
	})
	errMsg := ""
	if err := l.Error(); err != nil {
		errMsg = err.Error()
	}
	WriteJSON(w, http.StatusOK, map[string]any{"group": group, "state": l.State, "protocolType": l.ProtocolType,
		"members": len(l.Members), "totalLag": total, "partitions": rows, "error": errMsg})
}
