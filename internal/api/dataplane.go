// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/bvaliev/brokerium/internal/cluster"
	"github.com/bvaliev/brokerium/internal/kafka"
	"github.com/bvaliev/brokerium/internal/store"
)

func (s *Server) registerDataPlane() {
	s.Route("POST /api/v1/clusters/{id}/topics", store.RoleOperator, s.handleCreateTopic)
	s.Route("DELETE /api/v1/clusters/{id}/topics/{topic}", store.RoleOperator, s.handleDeleteTopic)
	s.Route("PATCH /api/v1/clusters/{id}/topics/{topic}/configs", store.RoleOperator, s.handleTopicConfigs)
	s.Route("POST /api/v1/clusters/{id}/topics/{topic}/partitions", store.RoleOperator, s.handleAddPartitions)
	s.Route("GET /api/v1/clusters/{id}/topics/{topic}/messages", store.RoleViewer, s.handleBrowse)
	s.Route("POST /api/v1/clusters/{id}/topics/{topic}/messages", store.RoleOperator, s.handleProduce)
	s.Route("PATCH /api/v1/clusters/{id}/brokers/{broker}/configs", store.RoleOperator, s.handleBrokerConfigsAlter)
	s.Route("GET /api/v1/clusters/{id}/acls", store.RoleViewer, s.handleListACLs)
	s.Route("POST /api/v1/clusters/{id}/acls", store.RoleOperator, s.handleCreateACL)
	s.Route("DELETE /api/v1/clusters/{id}/acls", store.RoleOperator, s.handleDeleteACL)
	s.Route("GET /api/v1/clusters/{id}/scram-users", store.RoleViewer, s.handleListSCRAM)
	s.Route("POST /api/v1/clusters/{id}/scram-users", store.RoleOperator, s.handleUpsertSCRAM)
	s.Route("DELETE /api/v1/clusters/{id}/scram-users/{user}", store.RoleOperator, s.handleDeleteSCRAM)
	s.Route("GET /api/v1/clusters/{id}/quotas", store.RoleViewer, s.handleListQuotas)
	s.Route("PUT /api/v1/clusters/{id}/quotas", store.RoleOperator, s.handleAlterQuota)
	s.Route("POST /api/v1/clusters/{id}/groups/{group}/reset-offsets", store.RoleOperator, s.handleResetOffsets)
	s.Route("DELETE /api/v1/clusters/{id}/groups/{group}", store.RoleOperator, s.handleDeleteGroup)
}

// capable returns the client of a cluster after checking a capability.
func (s *Server) capable(w http.ResponseWriter, r *http.Request, capability string) (*kafka.Client, *cluster.View, bool) {
	id := r.PathValue("id")
	v, err := s.Clusters.View(r.Context(), id, time.Minute)
	if err != nil {
		WriteStoreError(w, err)
		return nil, nil, false
	}
	if capability != "" && !hasCapability(v.Capabilities, capability) {
		WriteError(w, http.StatusForbidden, "not_supported", "this cluster does not support "+capability)
		return nil, nil, false
	}
	cl, _, err := s.Clusters.Client(r.Context(), id)
	if err != nil {
		WriteStoreError(w, err)
		return nil, nil, false
	}
	return cl, v, true
}

func hasCapability(c cluster.Capabilities, name string) bool {
	switch name {
	case "manageTopics":
		return c.ManageTopics
	case "manageConfigs":
		return c.ManageConfigs
	case "manageAcls":
		return c.ManageACLs
	case "manageScram":
		return c.ManageSCRAM
	case "manageQuotas":
		return c.ManageQuotas
	case "manageGroups":
		return c.ManageGroups
	case "browseMessages":
		return c.BrowseMessages
	}
	return false
}

// kafkaErr maps Kafka errors to HTTP: request problems the user can fix are 4xx, the rest 502.
func kafkaErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, kerr.SecurityDisabled):
		WriteError(w, http.StatusConflict, "security_disabled",
			"the cluster has no authorizer (authorizer.class.name), so ACLs cannot be managed")
	case errors.Is(err, kerr.TopicAuthorizationFailed), errors.Is(err, kerr.GroupAuthorizationFailed),
		errors.Is(err, kerr.ClusterAuthorizationFailed), errors.Is(err, kerr.TransactionalIDAuthorizationFailed):
		WriteError(w, http.StatusForbidden, "kafka_not_authorized", "the Brokerium principal is not allowed to do this: "+err.Error())
	case errors.Is(err, kerr.InvalidConfig), errors.Is(err, kerr.InvalidPartitions), errors.Is(err, kerr.InvalidReplicationFactor),
		errors.Is(err, kerr.InvalidTopicException), errors.Is(err, kerr.InvalidRequest), errors.Is(err, kerr.PolicyViolation):
		WriteError(w, http.StatusBadRequest, "kafka_invalid", err.Error())
	case errors.Is(err, kerr.TopicAlreadyExists):
		WriteError(w, http.StatusConflict, "already_exists", err.Error())
	case errors.Is(err, kerr.UnknownTopicOrPartition), errors.Is(err, kerr.GroupIDNotFound), errors.Is(err, kerr.ResourceNotFound):
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, kerr.NonEmptyGroup):
		WriteError(w, http.StatusConflict, "group_active", err.Error())
	default:
		WriteError(w, http.StatusBadGateway, "kafka_error", err.Error())
	}
}

func timeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 20*time.Second)
}

type createTopicRequest struct {
	Name              string            `json:"name"`
	Partitions        int32             `json:"partitions"`
	ReplicationFactor int16             `json:"replicationFactor"`
	Configs           map[string]string `json:"configs"`
}

func (s *Server) handleCreateTopic(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageTopics")
	if !ok {
		return
	}
	var req createTopicRequest
	if !Decode(w, r, &req) {
		return
	}
	if req.Name == "" || req.Partitions <= 0 || req.ReplicationFactor <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "name, partitions and replicationFactor are required")
		return
	}
	cfg := map[string]*string{}
	for k, v := range req.Configs {
		cfg[k] = &v
	}
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.CreateTopic(ctx, req.Partitions, req.ReplicationFactor, cfg, req.Name)
	if err == nil {
		err = resp.Err
	}
	s.Audit(r, "topic.create", r.PathValue("id"), req.Name, req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	go s.Clusters.Refresh(context.Background(), r.PathValue("id")) //nolint:errcheck
	WriteJSON(w, http.StatusCreated, map[string]any{"topic": req.Name, "id": resp.ID.String()})
}

func (s *Server) handleDeleteTopic(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageTopics")
	if !ok {
		return
	}
	topic := r.PathValue("topic")
	if r.URL.Query().Get("confirm") != topic {
		WriteError(w, http.StatusBadRequest, "confirmation_required", "pass ?confirm=<topic name> to delete the topic")
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.DeleteTopic(ctx, topic)
	if err == nil {
		err = resp.Err
	}
	s.Audit(r, "topic.delete", r.PathValue("id"), topic, nil, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	go s.Clusters.Refresh(context.Background(), r.PathValue("id")) //nolint:errcheck
	w.WriteHeader(http.StatusNoContent)
}

type configChange struct {
	Set    map[string]string `json:"set"`
	Delete []string          `json:"delete"`
}

func (c configChange) alters() []kadm.AlterConfig {
	var out []kadm.AlterConfig
	keys := make([]string, 0, len(c.Set))
	for k := range c.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.Set[k]
		out = append(out, kadm.AlterConfig{Op: kadm.SetConfig, Name: k, Value: &v})
	}
	for _, k := range c.Delete {
		out = append(out, kadm.AlterConfig{Op: kadm.DeleteConfig, Name: k})
	}
	return out
}

func (s *Server) handleTopicConfigs(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageConfigs")
	if !ok {
		return
	}
	var req configChange
	if !Decode(w, r, &req) {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	topic := r.PathValue("topic")
	resp, err := cl.Adm.AlterTopicConfigs(ctx, req.alters(), topic)
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "topic.configs", r.PathValue("id"), topic, req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddPartitions(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageTopics")
	if !ok {
		return
	}
	var req struct {
		Count int `json:"count"`
	}
	if !Decode(w, r, &req) {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	topic := r.PathValue("topic")
	resp, err := cl.Adm.UpdatePartitions(ctx, req.Count, topic)
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "topic.partitions", r.PathValue("id"), topic, req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	go s.Clusters.Refresh(context.Background(), r.PathValue("id")) //nolint:errcheck
	w.WriteHeader(http.StatusNoContent)
}

// MessageHeader is one record header.
type MessageHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Message is a record shown in the dashboard.
type Message struct {
	Partition     int32           `json:"partition"`
	Offset        int64           `json:"offset"`
	Timestamp     time.Time       `json:"timestamp"`
	Key           *string         `json:"key"`
	Value         *string         `json:"value"`
	Headers       []MessageHeader `json:"headers"`
	KeyEncoding   string          `json:"keyEncoding"`
	ValueEncoding string          `json:"valueEncoding"`
}

const maxValueBytes = 64 << 10

func encodeBytes(b []byte) (*string, string) {
	if b == nil {
		return nil, "utf8"
	}
	if len(b) > maxValueBytes {
		b = b[:maxValueBytes]
	}
	if utf8.Valid(b) {
		s := string(b)
		return &s, "utf8"
	}
	s := base64.StdEncoding.EncodeToString(b)
	return &s, "base64"
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	_, v, ok := s.capable(w, r, "browseMessages")
	if !ok {
		return
	}
	q := r.URL.Query()
	topic := r.PathValue("topic")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	partition := int32(-1)
	if p := q.Get("partition"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid partition")
			return
		}
		partition = int32(n)
	}
	from := q.Get("from")
	if from == "" {
		from = "latest"
	}
	var parts []int32
	for _, t := range v.Snapshot.Topics {
		if t.Name == topic {
			for _, p := range t.Partitions {
				if partition < 0 || p.ID == partition {
					parts = append(parts, p.ID)
				}
			}
		}
	}
	if len(parts) == 0 {
		WriteError(w, http.StatusNotFound, "not_found", "topic or partition not found")
		return
	}
	cl, _, err := s.Clusters.Client(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	starts, _ := cl.Adm.ListStartOffsets(ctx, topic)
	ends, _ := cl.Adm.ListEndOffsets(ctx, topic)
	perPartition := max(1, limit/len(parts))
	offsets := map[int32]kgo.Offset{}
	want := 0
	for _, p := range parts {
		start, end := int64(0), int64(0)
		if o, ok := starts.Lookup(topic, p); ok {
			start = o.Offset
		}
		if o, ok := ends.Lookup(topic, p); ok {
			end = o.Offset
		}
		var at int64
		switch from {
		case "earliest":
			at = start
		case "offset":
			n, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
			at = max(start, n)
		case "timestamp":
			ms, _ := strconv.ParseInt(q.Get("timestamp"), 10, 64)
			if lo, err := cl.Adm.ListOffsetsAfterMilli(ctx, ms, topic); err == nil {
				if o, ok := lo.Lookup(topic, p); ok {
					at = o.Offset
				}
			}
		default:
			at = max(start, end-int64(perPartition))
		}
		if at < end {
			offsets[p] = kgo.NewOffset().At(at)
			want += int(min(int64(perPartition), end-at))
		}
	}
	msgs := []Message{}
	if want > 0 {
		profile, err := s.profileOf(r)
		if err != nil {
			WriteStoreError(w, err)
			return
		}
		consumer, err := kafka.NewClient(profile, kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: offsets}),
			kgo.FetchMaxWait(500*time.Millisecond))
		if err != nil {
			kafkaErr(w, err)
			return
		}
		defer consumer.Close()
		perPart := map[int32]int{}
		deadline := time.Now().Add(5 * time.Second)
		for len(msgs) < min(want, limit) && time.Now().Before(deadline) {
			pctx, pcancel := context.WithTimeout(ctx, time.Until(deadline))
			fetches := consumer.KGO.PollFetches(pctx)
			pcancel()
			fetches.EachRecord(func(rec *kgo.Record) {
				if perPart[rec.Partition] >= perPartition || len(msgs) >= limit {
					return
				}
				perPart[rec.Partition]++
				m := Message{Partition: rec.Partition, Offset: rec.Offset, Timestamp: rec.Timestamp, Headers: []MessageHeader{}}
				m.Key, m.KeyEncoding = encodeBytes(rec.Key)
				m.Value, m.ValueEncoding = encodeBytes(rec.Value)
				for _, h := range rec.Headers {
					val, _ := encodeBytes(h.Value)
					if val == nil {
						empty := ""
						val = &empty
					}
					m.Headers = append(m.Headers, MessageHeader{Key: h.Key, Value: *val})
				}
				msgs = append(msgs, m)
			})
		}
	}
	sort.Slice(msgs, func(i, j int) bool {
		if msgs[i].Partition != msgs[j].Partition {
			return msgs[i].Partition < msgs[j].Partition
		}
		return msgs[i].Offset < msgs[j].Offset
	})
	WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) profileOf(r *http.Request) (kafka.Profile, error) {
	c, err := s.Store.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		return kafka.Profile{}, err
	}
	return s.Clusters.Profile(c)
}

type produceRequest struct {
	Partition *int32          `json:"partition"`
	Key       *string         `json:"key"`
	Value     string          `json:"value"`
	Headers   []MessageHeader `json:"headers"`
}

func (s *Server) handleProduce(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.capable(w, r, "browseMessages"); !ok {
		return
	}
	var req produceRequest
	if !Decode(w, r, &req) {
		return
	}
	profile, err := s.profileOf(r)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	opts := []kgo.Opt{kgo.RequiredAcks(kgo.AllISRAcks())}
	if req.Partition != nil {
		opts = append(opts, kgo.RecordPartitioner(kgo.ManualPartitioner()))
	}
	producer, err := kafka.NewClient(profile, opts...)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	defer producer.Close()
	rec := &kgo.Record{Topic: r.PathValue("topic"), Value: []byte(req.Value)}
	if req.Key != nil {
		rec.Key = []byte(*req.Key)
	}
	if req.Partition != nil {
		rec.Partition = *req.Partition
	}
	for _, h := range req.Headers {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: []byte(h.Value)})
	}
	ctx, cancel := timeout(r)
	defer cancel()
	err = producer.KGO.ProduceSync(ctx, rec).FirstErr()
	s.Audit(r, "topic.produce", r.PathValue("id"), rec.Topic, map[string]any{"partition": rec.Partition, "bytes": len(req.Value)}, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"partition": rec.Partition, "offset": rec.Offset})
}

func (s *Server) handleBrokerConfigsAlter(w http.ResponseWriter, r *http.Request) {
	cl, v, ok := s.capable(w, r, "manageConfigs")
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("broker"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid broker id")
		return
	}
	alive := false
	for _, b := range v.Snapshot.Brokers {
		alive = alive || (b.ID == int32(id) && b.Alive)
	}
	if !alive {
		WriteError(w, http.StatusConflict, "broker_offline", "dynamic configs can only be changed on a live broker")
		return
	}
	var req configChange
	if !Decode(w, r, &req) {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.AlterBrokerConfigs(ctx, req.alters(), int32(id))
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "broker.configs", r.PathValue("id"), r.PathValue("broker"), req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ACL is one access control entry.
type ACL struct {
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
	PatternType  string `json:"patternType"`
	Principal    string `json:"principal"`
	Host         string `json:"host"`
	Operation    string `json:"operation"`
	Permission   string `json:"permission"`
}

func (s *Server) handleListACLs(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageAcls")
	if !ok {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	b := kadm.NewACLs().AnyResource().ResourcePatternType(kadm.ACLPatternAny).Allow().Deny().AllowHosts().DenyHosts().
		Operations(kadm.OpAny)
	results, err := cl.Adm.DescribeACLs(ctx, b)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	out := []ACL{}
	for _, res := range results {
		if res.Err != nil {
			if strings.Contains(res.Err.Error(), "SECURITY_DISABLED") {
				WriteError(w, http.StatusConflict, "authorizer_disabled", "the cluster has no authorizer configured; ACLs are not enforced")
				return
			}
			kafkaErr(w, res.Err)
			return
		}
		for _, d := range res.Described {
			out = append(out, ACL{ResourceType: d.Type.String(), ResourceName: d.Name, PatternType: d.Pattern.String(),
				Principal: d.Principal, Host: d.Host, Operation: d.Operation.String(), Permission: d.Permission.String()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.ResourceType+a.ResourceName+a.Principal+a.Operation < b.ResourceType+b.ResourceName+b.Principal+b.Operation
	})
	WriteJSON(w, http.StatusOK, map[string]any{"acls": out})
}

func aclBuilder(a ACL) (*kadm.ACLBuilder, error) {
	b := kadm.NewACLs()
	switch strings.ToUpper(a.ResourceType) {
	case "TOPIC":
		b = b.Topics(a.ResourceName)
	case "GROUP":
		b = b.Groups(a.ResourceName)
	case "CLUSTER":
		b = b.Clusters()
	case "TRANSACTIONAL_ID":
		b = b.TransactionalIDs(a.ResourceName)
	case "DELEGATION_TOKEN":
		b = b.DelegationTokens(a.ResourceName)
	default:
		return nil, fmt.Errorf("unsupported resource type %q", a.ResourceType)
	}
	pattern, err := kmsg.ParseACLResourcePatternType(strings.ToUpper(orDefault(a.PatternType, "LITERAL")))
	if err != nil {
		return nil, err
	}
	b = b.ResourcePatternType(pattern)
	op, err := kmsg.ParseACLOperation(strings.ToUpper(a.Operation))
	if err != nil {
		return nil, err
	}
	b = b.Operations(op)
	host := orDefault(a.Host, "*")
	if !strings.HasPrefix(a.Principal, "User:") {
		return nil, errors.New("principal must look like User:<name>")
	}
	switch strings.ToUpper(a.Permission) {
	case "ALLOW":
		b = b.Allow(a.Principal).AllowHosts(host)
	case "DENY":
		b = b.Deny(a.Principal).DenyHosts(host)
	default:
		return nil, errors.New("permission must be ALLOW or DENY")
	}
	return b, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s *Server) handleCreateACL(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageAcls")
	if !ok {
		return
	}
	var req ACL
	if !Decode(w, r, &req) {
		return
	}
	b, err := aclBuilder(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	results, err := cl.Adm.CreateACLs(ctx, b)
	if err == nil {
		for _, res := range results {
			if res.Err != nil {
				err = res.Err
			}
		}
	}
	s.Audit(r, "acl.create", r.PathValue("id"), req.ResourceType+":"+req.ResourceName, req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, req)
}

func (s *Server) handleDeleteACL(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageAcls")
	if !ok {
		return
	}
	var req ACL
	if !Decode(w, r, &req) {
		return
	}
	b, err := aclBuilder(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	results, err := cl.Adm.DeleteACLs(ctx, b)
	if err == nil {
		for _, res := range results {
			if res.Err != nil {
				err = res.Err
			}
		}
	}
	s.Audit(r, "acl.delete", r.PathValue("id"), req.ResourceType+":"+req.ResourceName, req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSCRAM(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageScram")
	if !ok {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	described, err := cl.Adm.DescribeUserSCRAMs(ctx)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	type mech struct {
		Mechanism  string `json:"mechanism"`
		Iterations int32  `json:"iterations"`
	}
	type user struct {
		User       string `json:"user"`
		Mechanisms []mech `json:"mechanisms"`
	}
	out := []user{}
	for _, d := range described {
		if d.Err != nil {
			continue
		}
		u := user{User: d.User, Mechanisms: []mech{}}
		for _, c := range d.CredInfos {
			u.Mechanisms = append(u.Mechanisms, mech{Mechanism: c.Mechanism.String(), Iterations: c.Iterations})
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	WriteJSON(w, http.StatusOK, map[string]any{"users": out})
}

func scramMechanism(s string) (kadm.ScramMechanism, error) {
	switch strings.ToUpper(s) {
	case "SCRAM-SHA-256":
		return kadm.ScramSha256, nil
	case "SCRAM-SHA-512", "":
		return kadm.ScramSha512, nil
	}
	return 0, fmt.Errorf("unsupported mechanism %q", s)
}

func (s *Server) handleUpsertSCRAM(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageScram")
	if !ok {
		return
	}
	var req struct {
		User       string `json:"user"`
		Mechanism  string `json:"mechanism"`
		Password   string `json:"password"`
		Iterations int32  `json:"iterations"`
	}
	if !Decode(w, r, &req) {
		return
	}
	mech, err := scramMechanism(req.Mechanism)
	if err != nil || req.User == "" || len(req.Password) < 8 {
		WriteError(w, http.StatusBadRequest, "bad_request", "user, a password of 8+ characters and a SCRAM mechanism are required")
		return
	}
	if req.Iterations < 4096 {
		req.Iterations = 8192
	}
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.AlterUserSCRAMs(ctx, nil, []kadm.UpsertSCRAM{{User: req.User, Mechanism: mech, Iterations: req.Iterations, Password: req.Password}})
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "scram.upsert", r.PathValue("id"), req.User, map[string]any{"mechanism": mech.String(), "iterations": req.Iterations}, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"user": req.User, "mechanism": mech.String()})
}

func (s *Server) handleDeleteSCRAM(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageScram")
	if !ok {
		return
	}
	mech, err := scramMechanism(r.URL.Query().Get("mechanism"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	user := r.PathValue("user")
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.AlterUserSCRAMs(ctx, []kadm.DeleteSCRAM{{User: user, Mechanism: mech}}, nil)
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "scram.delete", r.PathValue("id"), user, map[string]any{"mechanism": mech.String()}, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// QuotaEntity identifies a quota target; a nil name is the default entity.
type QuotaEntity struct {
	Type string  `json:"type"`
	Name *string `json:"name"`
}

func (s *Server) handleListQuotas(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageQuotas")
	if !ok {
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	described, err := cl.Adm.DescribeClientQuotas(ctx, false, nil)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	type entry struct {
		Entity []QuotaEntity      `json:"entity"`
		Values map[string]float64 `json:"values"`
	}
	out := []entry{}
	for _, d := range described {
		e := entry{Values: map[string]float64{}}
		for _, c := range d.Entity {
			e.Entity = append(e.Entity, QuotaEntity{Type: c.Type, Name: c.Name})
		}
		for _, v := range d.Values {
			e.Values[v.Key] = v.Value
		}
		out = append(out, e)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"quotas": out})
}

func (s *Server) handleAlterQuota(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageQuotas")
	if !ok {
		return
	}
	var req struct {
		Entity []QuotaEntity      `json:"entity"`
		Set    map[string]float64 `json:"set"`
		Remove []string           `json:"remove"`
	}
	if !Decode(w, r, &req) {
		return
	}
	if len(req.Entity) == 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "entity is required")
		return
	}
	entry := kadm.AlterClientQuotaEntry{}
	for _, e := range req.Entity {
		entry.Entity = append(entry.Entity, kadm.ClientQuotaEntityComponent{Type: e.Type, Name: e.Name})
	}
	for k, v := range req.Set {
		entry.Ops = append(entry.Ops, kadm.AlterClientQuotaOp{Key: k, Value: v})
	}
	for _, k := range req.Remove {
		entry.Ops = append(entry.Ops, kadm.AlterClientQuotaOp{Key: k, Remove: true})
	}
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.AlterClientQuotas(ctx, []kadm.AlterClientQuotaEntry{entry})
	if err == nil {
		for _, rr := range resp {
			if rr.Err != nil {
				err = rr.Err
			}
		}
	}
	s.Audit(r, "quota.alter", r.PathValue("id"), fmt.Sprint(req.Entity), req, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resetRequest struct {
	Topic      string  `json:"topic"`
	Partitions []int32 `json:"partitions"`
	To         string  `json:"to"`
	Timestamp  int64   `json:"timestamp"`
	Offset     int64   `json:"offset"`
	DryRun     bool    `json:"dryRun"`
}

func (s *Server) handleResetOffsets(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageGroups")
	if !ok {
		return
	}
	var req resetRequest
	if !Decode(w, r, &req) {
		return
	}
	group := r.PathValue("group")
	ctx, cancel := timeout(r)
	defer cancel()
	share := cl.ShareGroupNames(ctx)[group]
	if !req.DryRun {
		members, err := groupMembers(ctx, cl, group, share)
		if err != nil {
			kafkaErr(w, err)
			return
		}
		if members > 0 {
			WriteError(w, http.StatusConflict, "group_active", "the group has active members; stop the consumers before resetting offsets")
			return
		}
	}
	var listed kadm.ListedOffsets
	var err error
	switch req.To {
	case "earliest":
		listed, err = cl.Adm.ListStartOffsets(ctx, req.Topic)
	case "latest":
		listed, err = cl.Adm.ListEndOffsets(ctx, req.Topic)
	case "timestamp":
		listed, err = cl.Adm.ListOffsetsAfterMilli(ctx, req.Timestamp, req.Topic)
	case "offset":
		listed, err = cl.Adm.ListEndOffsets(ctx, req.Topic)
	default:
		WriteError(w, http.StatusBadRequest, "bad_request", "to must be earliest, latest, timestamp or offset")
		return
	}
	if err != nil {
		kafkaErr(w, err)
		return
	}
	only := map[int32]bool{}
	for _, p := range req.Partitions {
		only[p] = true
	}
	committed := currentOffsets(ctx, cl, group, share)
	type change struct {
		Topic     string `json:"topic"`
		Partition int32  `json:"partition"`
		From      int64  `json:"from"`
		To        int64  `json:"to"`
	}
	changes := []change{}
	commit := kadm.Offsets{}
	listed.Each(func(o kadm.ListedOffset) {
		if len(only) > 0 && !only[o.Partition] {
			return
		}
		target := o.Offset
		if req.To == "offset" {
			target = min(req.Offset, o.Offset)
		}
		from := int64(-1)
		if at, ok := committed[fmt.Sprintf("%s-%d", req.Topic, o.Partition)]; ok {
			from = at
		}
		changes = append(changes, change{Topic: req.Topic, Partition: o.Partition, From: from, To: target})
		commit.AddOffset(req.Topic, o.Partition, target, -1)
	})
	sort.Slice(changes, func(i, j int) bool { return changes[i].Partition < changes[j].Partition })
	if !req.DryRun {
		var err error
		if share {
			var resp kadm.AlterShareGroupOffsetsResponses
			if resp, err = cl.Adm.AlterShareGroupOffsets(ctx, group, commit); err == nil {
				err = resp.Error()
			}
		} else {
			var resp kadm.OffsetResponses
			if resp, err = cl.Adm.CommitOffsets(ctx, group, commit); err == nil {
				err = resp.Error()
			}
		}
		s.Audit(r, "group.reset-offsets", r.PathValue("id"), group, req, err)
		if err != nil {
			kafkaErr(w, err)
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"offsets": changes, "dryRun": req.DryRun})
}

func groupMembers(ctx context.Context, cl *kafka.Client, group string, share bool) (int, error) {
	if share {
		described, err := cl.Adm.DescribeShareGroups(ctx, group)
		if err != nil && len(described) == 0 {
			return 0, err
		}
		return len(described[group].Members), nil
	}
	described, err := cl.Adm.DescribeGroups(ctx, group)
	if err != nil {
		return 0, err
	}
	return len(described[group].Members), nil
}

// currentOffsets returns committed offsets (consumer groups) or share-partition start offsets
// (share groups) keyed by "topic-partition".
func currentOffsets(ctx context.Context, cl *kafka.Client, group string, share bool) map[string]int64 {
	out := map[string]int64{}
	if share {
		described, _ := cl.Adm.DescribeShareGroupOffsets(ctx, group)
		for t, parts := range described[group].Offsets {
			for p, o := range parts {
				out[fmt.Sprintf("%s-%d", t, p)] = o.StartOffset
			}
		}
		return out
	}
	fetched, _ := cl.Adm.FetchOffsets(ctx, group)
	fetched.Each(func(o kadm.OffsetResponse) {
		out[fmt.Sprintf("%s-%d", o.Topic, o.Partition)] = o.At
	})
	return out
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.capable(w, r, "manageGroups")
	if !ok {
		return
	}
	group := r.PathValue("group")
	ctx, cancel := timeout(r)
	defer cancel()
	resp, err := cl.Adm.DeleteGroup(ctx, group)
	if err == nil {
		err = resp.Err
	}
	s.Audit(r, "group.delete", r.PathValue("id"), group, nil, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
