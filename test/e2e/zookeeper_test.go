// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestZooKeeperClusters registers the lab clusters delta (Kafka 3.5) and epsilon (Kafka 3.9),
// which run in ZooKeeper mode, and checks what Brokerium detects and offers on them: the mode
// and versions, the capabilities, health without quorum findings, topics, messages, groups,
// ACLs and an operation behind the health gate. Clusters whose brokers do not answer are
// skipped.
func TestZooKeeperClusters(t *testing.T) {
	for _, tc := range []struct{ name, bootstrap, version string }{
		{"delta", "localhost:61101", "3.5"},
		{"epsilon", "localhost:62101", "3.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", tc.bootstrap, 2*time.Second)
			if err != nil {
				t.Skipf("the lab cluster %s is not running (%v); start it with deploy/lab/lab.sh up %s", tc.name, err, tc.name)
			}
			_ = conn.Close()
			zookeeperScenario(t, newClient(t), tc.name, tc.bootstrap, tc.version)
		})
	}
}

type zkView struct {
	Health struct {
		Status        string   `json:"status"`
		Mode          string   `json:"mode"`
		Controller    int32    `json:"controller"`
		Versions      []string `json:"versions"`
		MixedVersions bool     `json:"mixedVersions"`
		Issues        []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"issues"`
	} `json:"health"`
	Capabilities map[string]any `json:"capabilities"`
	Snapshot     struct {
		Mode    string   `json:"mode"`
		Errors  []string `json:"errors"`
		Skipped []string `json:"skipped"`
		Quorum  any      `json:"quorum"`
		Brokers []struct {
			ID                         int32  `json:"id"`
			Version                    string `json:"version"`
			VersionExact               bool   `json:"versionExact"`
			Mode                       string `json:"mode"`
			ZooKeeperConnect           string `json:"zookeeperConnect"`
			InterBrokerProtocolVersion string `json:"interBrokerProtocolVersion"`
		} `json:"brokers"`
		Topics []struct {
			Name       string `json:"name"`
			Partitions []struct {
				ID       int32   `json:"id"`
				Leader   int32   `json:"leader"`
				Replicas []int32 `json:"replicas"`
			} `json:"partitions"`
		} `json:"topics"`
		Groups []zkGroup `json:"groups"`
	} `json:"snapshot"`
}

type zkGroup struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// registered returns the id of the cluster named name, registering it first when needed.
func (c *client) registered(name, bootstrap string) string {
	c.t.Helper()
	var list struct {
		Clusters []summary `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters", nil, &list)
	for _, s := range list.Clusters {
		if s.Cluster.Name == name {
			return s.Cluster.ID
		}
	}
	var v struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters", map[string]any{"name": name, "bootstrapServers": []string{bootstrap}}, &v)
	return v.Cluster.ID
}

func zookeeperScenario(t *testing.T, c *client, name, bootstrap, version string) {
	id := c.registered(name, bootstrap)
	base := "/api/v1/clusters/" + id
	zkv := func() zkView {
		t.Helper()
		var v zkView
		c.must(http.StatusOK, "GET", base+"?fresh=1", nil, &v)
		return v
	}
	waitZK := func(what string, cond func(zkView) bool) zkView {
		t.Helper()
		deadline := time.Now().Add(time.Minute)
		for {
			v := zkv()
			if cond(v) {
				return v
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: %+v", what, v.Health)
			}
			time.Sleep(2 * time.Second)
		}
	}

	// Detection: the mode and the version of every broker, from ApiVersions and DescribeConfigs.
	v := zkv()
	if v.Health.Mode != "zookeeper" || v.Snapshot.Mode != "zookeeper" {
		t.Fatalf("mode %q (snapshot %q), want zookeeper", v.Health.Mode, v.Snapshot.Mode)
	}
	if !slices.Equal(v.Health.Versions, []string{version}) || v.Health.MixedVersions {
		t.Errorf("versions %v (mixed %v), want [%s]", v.Health.Versions, v.Health.MixedVersions, version)
	}
	if len(v.Snapshot.Brokers) != 3 {
		t.Fatalf("%d brokers, want 3", len(v.Snapshot.Brokers))
	}
	for _, b := range v.Snapshot.Brokers {
		if b.Mode != "zookeeper" || b.Version != version || b.VersionExact || b.ZooKeeperConnect != name+"-zk:2181" ||
			!strings.HasPrefix(b.InterBrokerProtocolVersion, version+"-IV") {
			t.Errorf("broker %+v", b)
		}
	}
	if v.Health.Controller < 101 || v.Health.Controller > 103 {
		t.Errorf("controller %d, want one of the brokers", v.Health.Controller)
	}

	// Health without the KRaft parts that do not apply.
	if v.Health.Status != "ok" || len(v.Snapshot.Errors) > 0 || v.Snapshot.Quorum != nil || !slices.Contains(v.Snapshot.Skipped, "quorum") {
		t.Errorf("status %s, issues %+v, snapshot errors %v, skipped %v", v.Health.Status, v.Health.Issues, v.Snapshot.Errors, v.Snapshot.Skipped)
	}

	// Capabilities: the data plane and placement, nothing that needs KRaft.
	for key, want := range map[string]bool{
		"manageTopics": true, "manageConfigs": true, "manageAcls": true, "manageGroups": true, "browseMessages": true,
		"reassign": true, "electLeaders": true,
		"cordon": false, "unregisterBrokers": false, "describeQuorum": false, "removeVoters": false, "addControllers": false,
		"finalizeFeatures": false, "migrateQuorum": false, "restartNodes": false, "upgradeVersion": false,
	} {
		if got, _ := v.Capabilities[key].(bool); got != want {
			t.Errorf("capability %s = %v, want %v", key, got, want)
		}
	}
	if status, raw := c.send("POST", base+"/operations", map[string]any{"type": "drain-broker", "params": map[string]any{"brokerId": 101}}); status != http.StatusBadRequest ||
		!strings.Contains(string(raw), "cordon") {
		t.Errorf("drain-broker on ZooKeeper: %d %s", status, raw)
	}

	// Topics: create with a config, change it, add partitions. In ZooKeeper mode the controller
	// tells the other brokers about a new topic a moment after it was created.
	topic := uniqueName("e2e-zk-")
	c.must(http.StatusCreated, "POST", base+"/topics", map[string]any{"name": topic, "partitions": 2, "replicationFactor": 3,
		"configs": map[string]string{"retention.ms": "3600000"}}, nil)
	defer c.do("DELETE", base+"/topics/"+topic+"?confirm="+topic, nil, nil)
	var detail struct {
		Topic struct {
			Partitions []any             `json:"partitions"`
			Configs    map[string]string `json:"configs"`
		} `json:"topic"`
	}
	waitTopic := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			c.must(http.StatusOK, "POST", base+"/refresh", nil, nil)
			if c.do("GET", base+"/topics/"+topic, nil, &detail) == http.StatusOK && cond() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("topic %s: timed out waiting for %s: %+v", topic, what, detail.Topic)
			}
			time.Sleep(time.Second)
		}
	}
	waitTopic("the new topic", func() bool {
		return len(detail.Topic.Partitions) == 2 && detail.Topic.Configs["retention.ms"] == "3600000"
	})
	c.must(http.StatusNoContent, "PATCH", base+"/topics/"+topic+"/configs", map[string]any{"set": map[string]string{"retention.ms": "7200000"}}, nil)
	c.must(http.StatusNoContent, "POST", base+"/topics/"+topic+"/partitions", map[string]any{"count": 3}, nil)
	waitTopic("3 partitions and the new config", func() bool {
		return len(detail.Topic.Partitions) == 3 && detail.Topic.Configs["retention.ms"] == "7200000"
	})

	// Messages: produce and read back.
	messages := base + "/topics/" + topic + "/messages"
	for i := 0; i < 3; i++ {
		c.must(http.StatusCreated, "POST", messages, map[string]any{"partition": 0, "key": "k", "value": "hello"}, nil)
	}
	var browsed struct {
		Messages []struct {
			Value *string `json:"value"`
		} `json:"messages"`
	}
	c.must(http.StatusOK, "GET", messages+"?partition=0&from=earliest&limit=10", nil, &browsed)
	if len(browsed.Messages) != 3 || browsed.Messages[0].Value == nil || *browsed.Messages[0].Value != "hello" {
		t.Fatalf("browsed %+v", browsed.Messages)
	}

	// Groups: a consumer group is listed as one, also by brokers before ListGroups v5, which
	// ignore the type filter of share groups.
	group := topic + "-group"
	consume(t, bootstrap, topic, group, 3)
	v = waitZK("the consumer group", func(v zkView) bool {
		return slices.ContainsFunc(v.Snapshot.Groups, func(g zkGroup) bool { return g.ID == group })
	})
	for _, g := range v.Snapshot.Groups {
		if g.Type != "consumer" {
			t.Errorf("group %s has type %q, want consumer", g.ID, g.Type)
		}
	}
	var g struct {
		Type     string `json:"type"`
		TotalLag int64  `json:"totalLag"`
	}
	c.must(http.StatusOK, "GET", base+"/groups/"+group, nil, &g)
	if g.Type != "consumer" || g.TotalLag != 0 {
		t.Errorf("group detail %+v, want a consumer group without lag", g)
	}
	c.must(http.StatusNoContent, "DELETE", base+"/groups/"+group, nil, nil)

	// ACLs with the AclAuthorizer of ZooKeeper mode.
	acl := map[string]any{"resourceType": "TOPIC", "resourceName": topic, "patternType": "LITERAL",
		"principal": "User:e2e", "host": "*", "operation": "READ", "permission": "ALLOW"}
	c.must(http.StatusCreated, "POST", base+"/acls", acl, nil)
	var acls struct {
		ACLs []map[string]any `json:"acls"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for !slices.ContainsFunc(acls.ACLs, func(a map[string]any) bool { return a["resourceName"] == topic && a["principal"] == "User:e2e" }) {
		if time.Now().After(deadline) {
			t.Fatalf("the ACL is not listed: %+v", acls.ACLs)
		}
		time.Sleep(time.Second)
		c.must(http.StatusOK, "GET", base+"/acls", nil, &acls)
	}
	c.must(http.StatusNoContent, "DELETE", base+"/acls", acl, nil)

	// An operation behind the health gate: swap the leader of partition 0 by reassignment.
	v = zkv()
	var replicas []int32
	for _, tp := range v.Snapshot.Topics {
		if tp.Name == topic {
			replicas = slices.Clone(tp.Partitions[0].Replicas)
		}
	}
	if len(replicas) != 3 {
		t.Fatalf("replicas of %s-0: %v", topic, replicas)
	}
	replicas[0], replicas[1] = replicas[1], replicas[0]
	op := c.run(id, name, "move-partitions", map[string]any{"moves": []map[string]any{{"topic": topic, "partition": 0, "replicas": replicas}}},
		5*time.Minute)
	gated := false
	for _, s := range op.Steps {
		gated = gated || (s.Kind == "health-gate" && s.Status == "succeeded")
	}
	if !gated {
		t.Errorf("no health gate passed: %+v", op.Steps)
	}
	waitZK("the new leader", func(v zkView) bool {
		for _, tp := range v.Snapshot.Topics {
			if tp.Name == topic {
				return tp.Partitions[0].Leader == replicas[0]
			}
		}
		return false
	})
	c.must(http.StatusNoContent, "DELETE", base+"/topics/"+topic+"?confirm="+topic, nil, nil)
}
