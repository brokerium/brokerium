// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// consume reads n records of topic as group and commits them, which creates the group.
func consume(t *testing.T, bootstrap, topic, group string, n int) {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(bootstrap), kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.DisableAutoCommit())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	got := 0
	for got < n {
		fetches := cl.PollFetches(ctx)
		if err := ctx.Err(); err != nil {
			t.Fatalf("consumed %d of %d records: %v", got, n, err)
		}
		got += fetches.NumRecords()
		if err := cl.CommitUncommittedOffsets(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDataPlane is scenario 9 against the lab cluster beta: topics, configs, partitions,
// messages, ACLs, SCRAM users, quotas and consumer groups with offset resets.
func TestDataPlane(t *testing.T) {
	c := newClient(t)
	beta := c.clusterID("beta")
	base := "/api/v1/clusters/" + beta
	topic := uniqueName("e2e-dp-")

	c.must(http.StatusCreated, "POST", base+"/topics", map[string]any{"name": topic, "partitions": 2, "replicationFactor": 1,
		"configs": map[string]string{"retention.ms": "3600000"}}, nil)
	defer c.do("DELETE", base+"/topics/"+topic+"?confirm="+topic, nil, nil)

	c.must(http.StatusNoContent, "PATCH", base+"/topics/"+topic+"/configs", map[string]any{"set": map[string]string{"cleanup.policy": "compact"}}, nil)
	c.must(http.StatusNoContent, "POST", base+"/topics/"+topic+"/partitions", map[string]any{"count": 3}, nil)
	var detail struct {
		Topic struct {
			Partitions []any             `json:"partitions"`
			Configs    map[string]string `json:"configs"`
		} `json:"topic"`
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(time.Second)
		}
	}
	waitFor("3 partitions and the new config", func() bool {
		c.must(http.StatusOK, "POST", base+"/refresh", nil, nil)
		return c.do("GET", base+"/topics/"+topic, nil, &detail) == http.StatusOK &&
			len(detail.Topic.Partitions) == 3 && detail.Topic.Configs["cleanup.policy"] == "compact"
	})

	// Messages: produce with a key and headers, browse them back.
	for i := 0; i < 5; i++ {
		c.must(http.StatusCreated, "POST", base+"/topics/"+topic+"/messages", map[string]any{"partition": 0, "key": "k",
			"value": "hello", "headers": []map[string]string{{"key": "trace", "value": "e2e"}}}, nil)
	}
	var browsed struct {
		Messages []struct {
			Key     *string `json:"key"`
			Value   *string `json:"value"`
			Headers []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"messages"`
	}
	c.must(http.StatusOK, "GET", base+"/topics/"+topic+"/messages?partition=0&from=earliest&limit=10", nil, &browsed)
	if len(browsed.Messages) != 5 || *browsed.Messages[0].Value != "hello" || len(browsed.Messages[0].Headers) != 1 {
		t.Fatalf("browsed %+v", browsed.Messages)
	}

	// Tombstones and binary keys, values and headers.
	messages := base + "/topics/" + topic + "/messages"
	c.must(http.StatusCreated, "POST", messages, map[string]any{"partition": 1, "key": "k", "value": nil}, nil)
	c.must(http.StatusCreated, "POST", messages, map[string]any{"partition": 1, "key": "/wA=", "keyEncoding": "base64",
		"value": "/w==", "valueEncoding": "base64", "headers": []map[string]string{{"key": "bin", "value": "AP8=", "encoding": "base64"}}}, nil)
	c.must(http.StatusBadRequest, "POST", messages, map[string]any{"partition": 1, "value": "%%%", "valueEncoding": "base64"}, nil)
	c.must(http.StatusBadRequest, "POST", messages, map[string]any{"partition": 1, "key": "k"}, nil)
	type page struct {
		Messages []struct {
			Offset        int64   `json:"offset"`
			Key           *string `json:"key"`
			KeyEncoding   string  `json:"keyEncoding"`
			Value         *string `json:"value"`
			ValueEncoding string  `json:"valueEncoding"`
			Headers       []struct {
				Value    string `json:"value"`
				Encoding string `json:"encoding"`
			} `json:"headers"`
		} `json:"messages"`
		Next     *string `json:"next"`
		Previous *string `json:"previous"`
	}
	var binary page
	c.must(http.StatusOK, "GET", messages+"?partition=1&from=earliest&limit=10", nil, &binary)
	if len(binary.Messages) != 2 || binary.Messages[0].Value != nil {
		t.Fatalf("tombstone: %+v", binary.Messages)
	}
	if m := binary.Messages[1]; m.KeyEncoding != "base64" || *m.Key != "/wA=" || m.ValueEncoding != "base64" || *m.Value != "/w==" ||
		len(m.Headers) != 1 || m.Headers[0].Encoding != "base64" || m.Headers[0].Value != "AP8=" {
		t.Fatalf("binary record: %+v", m)
	}

	// Paging through the five records of partition 0.
	offsetsOf := func(p page) []int64 {
		var out []int64
		for _, m := range p.Messages {
			out = append(out, m.Offset)
		}
		return out
	}
	var first, second, back, last page
	c.must(http.StatusOK, "GET", messages+"?partition=0&from=earliest&limit=2", nil, &first)
	if got := offsetsOf(first); !slices.Equal(got, []int64{0, 1}) || first.Next == nil || *first.Next != "0:2" || first.Previous != nil {
		t.Fatalf("first page %v, next %v, previous %v", got, first.Next, first.Previous)
	}
	c.must(http.StatusOK, "GET", messages+"?from=after&limit=2&cursor="+*first.Next, nil, &second)
	if got := offsetsOf(second); !slices.Equal(got, []int64{2, 3}) || second.Previous == nil || *second.Previous != "0:2" {
		t.Fatalf("second page %v, previous %v", got, second.Previous)
	}
	c.must(http.StatusOK, "GET", messages+"?from=before&limit=2&cursor="+*second.Previous, nil, &back)
	if got := offsetsOf(back); !slices.Equal(got, []int64{0, 1}) {
		t.Fatalf("page before %v", got)
	}
	c.must(http.StatusOK, "GET", messages+"?from=after&limit=2&cursor="+*second.Next, nil, &last)
	if got := offsetsOf(last); !slices.Equal(got, []int64{4}) || last.Next != nil {
		t.Fatalf("last page %v, next %v", got, last.Next)
	}
	c.must(http.StatusBadRequest, "GET", messages+"?from=after&cursor=x", nil, nil)

	// ACLs.
	acl := map[string]any{"resourceType": "TOPIC", "resourceName": topic, "patternType": "LITERAL",
		"principal": "User:e2e", "host": "*", "operation": "READ", "permission": "ALLOW"}
	c.must(http.StatusCreated, "POST", base+"/acls", acl, nil)
	var acls struct {
		ACLs []map[string]any `json:"acls"`
	}
	waitFor("the ACL", func() bool {
		c.must(http.StatusOK, "GET", base+"/acls", nil, &acls)
		for _, a := range acls.ACLs {
			if a["resourceName"] == topic && a["principal"] == "User:e2e" {
				return true
			}
		}
		return false
	})
	c.must(http.StatusNoContent, "DELETE", base+"/acls", acl, nil)

	// SCRAM users.
	c.must(http.StatusCreated, "POST", base+"/scram-users", map[string]any{"user": "e2e-dp", "mechanism": "SCRAM-SHA-256",
		"password": "e2e-secret-2", "iterations": 8192}, nil)
	var users struct {
		Users []struct {
			User string `json:"user"`
		} `json:"users"`
	}
	c.must(http.StatusOK, "GET", base+"/scram-users", nil, &users)
	found := false
	for _, u := range users.Users {
		found = found || u.User == "e2e-dp"
	}
	if !found {
		t.Errorf("SCRAM user missing from %+v", users.Users)
	}
	c.must(http.StatusNoContent, "DELETE", base+"/scram-users/e2e-dp?mechanism=SCRAM-SHA-256", nil, nil)

	// Quotas.
	entity := []map[string]any{{"type": "client-id", "name": "e2e-client"}}
	c.must(http.StatusNoContent, "PUT", base+"/quotas", map[string]any{"entity": entity, "set": map[string]float64{"producer_byte_rate": 1048576}}, nil)
	var quotas struct {
		Quotas []struct {
			Values map[string]float64 `json:"values"`
		} `json:"quotas"`
	}
	c.must(http.StatusOK, "GET", base+"/quotas", nil, &quotas)
	found = false
	for _, q := range quotas.Quotas {
		found = found || q.Values["producer_byte_rate"] == 1048576
	}
	if !found {
		t.Errorf("quota missing from %+v", quotas.Quotas)
	}
	c.must(http.StatusNoContent, "PUT", base+"/quotas", map[string]any{"entity": entity, "remove": []string{"producer_byte_rate"}}, nil)

	// Consumer group: consume, then rewind to the start (dry run first) and delete the group.
	group := topic + "-group"
	consume(t, "localhost:29101", topic, group, 5)
	reset := func(dry bool) []struct{ From, To int64 } {
		var res struct {
			Offsets []struct{ From, To int64 } `json:"offsets"`
		}
		c.must(http.StatusOK, "POST", base+"/groups/"+group+"/reset-offsets", map[string]any{"topic": topic, "to": "earliest", "dryRun": dry}, &res)
		return res.Offsets
	}
	plan := reset(true)
	if len(plan) != 3 {
		t.Fatalf("reset plan = %+v", plan)
	}
	reset(false)
	var g struct {
		TotalLag int64 `json:"totalLag"`
	}
	c.must(http.StatusOK, "GET", base+"/groups/"+group, nil, &g)
	if g.TotalLag != 5 {
		t.Errorf("lag after rewinding = %d, want 5", g.TotalLag)
	}
	c.must(http.StatusNoContent, "DELETE", base+"/groups/"+group, nil, nil)

	// Deleting a topic needs its name as confirmation.
	if got := c.do("DELETE", base+"/topics/"+topic, nil, nil); got != http.StatusBadRequest {
		t.Errorf("topic deletion without confirmation: status %d", got)
	}
	c.must(http.StatusNoContent, "DELETE", base+"/topics/"+topic+"?confirm="+topic, nil, nil)
	missing := topic + "-missing"
	if got := c.do("DELETE", base+"/topics/"+missing+"?confirm="+missing, nil, nil); got != http.StatusNotFound {
		t.Errorf("deleting a topic that never existed: status %d, want 404", got)
	}

	// Partitions and replication factor 0 take the broker defaults.
	defaults := topic + "-defaults"
	c.must(http.StatusCreated, "POST", base+"/topics", map[string]any{"name": defaults, "partitions": 0, "replicationFactor": 0}, nil)
	c.must(http.StatusNoContent, "DELETE", base+"/topics/"+defaults+"?confirm="+defaults, nil, nil)
}
