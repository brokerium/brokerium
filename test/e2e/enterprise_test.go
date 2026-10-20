// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e && enterprise

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/ee/licensing"
	"github.com/brokerium/brokerium/internal/edition"
)

// e2eCustomer is the customer of the license keys the Enterprise scenarios sign.
const e2eCustomer = "Brokerium e2e"

type licenseView struct {
	Edition  string `json:"edition"`
	Features []struct {
		ID       string `json:"id"`
		Licensed bool   `json:"licensed"`
	} `json:"features"`
	License *struct {
		Customer string `json:"customer"`
		State    string `json:"state"`
		Source   string `json:"source"`
	} `json:"license"`
}

func (v licenseView) licensed(feature string) bool {
	for _, f := range v.Features {
		if f.ID == feature {
			return f.Licensed
		}
	}
	return false
}

// testSigningKey returns the test signing key, whose seed is public. Only servers built with the
// licensetest tag trust it.
func testSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "ee", "licensing", "testdata", "test-signing.key"))
	if err != nil {
		t.Fatal(err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("test-signing.key is not a base64 Ed25519 seed: %v", err)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// signLicense signs a license key with the key id of the test key that covers the message tools
// for a day.
func signLicense(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	now := time.Now()
	key, err := licensing.Sign(licensing.Claims{V: licensing.Version, ID: uniqueName("e2e-"), KID: "test",
		Customer: e2eCustomer, Email: "e2e@example.com", Edition: edition.Enterprise, Features: []string{edition.MessageTools},
		IssuedAt: now, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour)}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// errorCode returns the code of an error response of the API.
func errorCode(raw []byte) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Error.Code
}

type foundMessages struct {
	Messages []struct {
		Partition int32  `json:"partition"`
		Offset    int64  `json:"offset"`
		Key       string `json:"key"`
	} `json:"messages"`
	Complete bool   `json:"complete"`
	Stopped  string `json:"stopped"`
}

type deletion struct {
	Partitions []struct {
		Partition    int32 `json:"partition"`
		DeleteBefore int64 `json:"deleteBefore"`
	} `json:"partitions"`
	Records int64 `json:"records"`
	DryRun  bool  `json:"dryRun"`
}

// TestEnterpriseMessageTools covers the message tools of Brokerium Enterprise on the lab cluster
// beta: the server refuses them without a license, a license key signed with the test key unlocks
// search, export, record deletion and copy-messages, and removing the key locks them again. Run it
// against a server built with -tags enterprise,licensetest:
//
//	go test -tags e2e,enterprise -run TestEnterprise ./test/e2e
func TestEnterpriseMessageTools(t *testing.T) {
	c := newClient(t)
	var lic licenseView
	c.must(http.StatusOK, "GET", "/api/v1/license", nil, &lic)
	if lic.Edition != edition.Enterprise {
		t.Skipf("the server runs the %s edition", lic.Edition)
	}
	if lic.License != nil {
		if lic.License.Source != edition.SourceDatabase || lic.License.Customer != e2eCustomer {
			t.Skipf("the server has a license key (source %s); run against a server without one", lic.License.Source)
		}
		// Left over by an interrupted run.
		c.must(http.StatusNoContent, "DELETE", "/api/v1/license", nil, nil)
	}
	beta := c.clusterID("beta")
	base := "/api/v1/clusters/" + beta
	topic := uniqueName("e2e-tools-")
	target := topic + "-copy"
	for _, name := range []string{topic, target} {
		c.must(http.StatusCreated, "POST", base+"/topics", map[string]any{"name": name, "partitions": 3, "replicationFactor": 1}, nil)
		t.Cleanup(func() { c.do("DELETE", base+"/topics/"+name+"?confirm="+name, nil, nil) })
	}

	// Record i goes to partition i%3 with the key order-<i>, the status REFUNDED for every fifth
	// record and PAID otherwise, and a parity header; the timestamps are one second apart.
	var cluster struct {
		Cluster struct {
			BootstrapServers []string `json:"bootstrapServers"`
		} `json:"cluster"`
	}
	c.must(http.StatusOK, "GET", base, nil, &cluster)
	kc, err := kgo.NewClient(kgo.SeedBrokers(cluster.Cluster.BootstrapServers...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	defer kc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	at := func(i int) int64 { return start.Add(time.Duration(i) * time.Second).UnixMilli() }
	var records []*kgo.Record
	for i := range 30 {
		status := "PAID"
		if i%5 == 0 {
			status = "REFUNDED"
		}
		records = append(records, &kgo.Record{Topic: topic, Partition: int32(i % 3), Key: fmt.Appendf(nil, "order-%d", i),
			Value: fmt.Appendf(nil, `{"order":%d,"status":%q}`, i, status), Timestamp: time.UnixMilli(at(i)),
			Headers: []kgo.RecordHeader{{Key: "parity", Value: []byte([]string{"even", "odd"}[i%2])}}})
	}
	if err := kc.ProduceSync(ctx, records...).FirstErr(); err != nil {
		t.Fatal(err)
	}

	evenRecords := map[string]any{"key": "parity", "value": map[string]any{"contains": "even"}}
	copyParams := map[string]any{"topic": topic, "targetTopic": target, "header": evenRecords}
	tools := []struct {
		path string
		body any
	}{
		{base + "/topics/" + topic + "/search", map[string]any{}},
		{base + "/topics/" + topic + "/export", map[string]any{}},
		{base + "/topics/" + topic + "/delete-records", map[string]any{"all": true, "dryRun": true}},
		{base + "/operations", map[string]any{"type": "copy-messages", "params": copyParams}},
	}
	refused := func(when string) {
		t.Helper()
		for _, tool := range tools {
			if status, raw := c.send("POST", tool.path, tool.body); status != http.StatusForbidden || errorCode(raw) != "license_required" {
				t.Errorf("%s: POST %s = %d %s, want 403 license_required", when, tool.path, status, raw)
			}
		}
	}
	refused("without a license")

	// A key signed by another key is refused; a key of the test key unlocks the message tools.
	_, other, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if status, raw := c.send("PUT", "/api/v1/license", map[string]string{"key": signLicense(t, other)}); status != http.StatusBadRequest ||
		errorCode(raw) != "invalid_license" {
		t.Errorf("a key of another signing key: %d %s, want 400 invalid_license", status, raw)
	}
	if status, raw := c.send("PUT", "/api/v1/license", map[string]string{"key": signLicense(t, testSigningKey(t))}); status != http.StatusOK {
		t.Fatalf("adding a key of the test key: %d %s; is the server built with -tags enterprise,licensetest?", status, raw)
	}
	t.Cleanup(func() { c.do("DELETE", "/api/v1/license", nil, nil) })
	c.must(http.StatusOK, "GET", "/api/v1/license", nil, &lic)
	if lic.License == nil || lic.License.State != edition.StateValid || lic.License.Source != edition.SourceDatabase ||
		lic.License.Customer != e2eCustomer || !lic.licensed(edition.MessageTools) {
		t.Fatalf("license %+v, features %+v", lic.License, lic.Features)
	}

	search := func(name string, req map[string]any) foundMessages {
		t.Helper()
		var res foundMessages
		c.must(http.StatusOK, "POST", base+"/topics/"+name+"/search", req, &res)
		return res
	}
	for _, s := range []struct {
		name string
		req  map[string]any
		want int
	}{
		{"header", map[string]any{"header": map[string]any{"key": "parity", "value": map[string]any{"contains": "odd"}}}, 15},
		{"key, ignoring case", map[string]any{"key": map[string]any{"contains": "ORDER-1"}, "ignoreCase": true}, 11},
		{"value", map[string]any{"value": map[string]any{"regex": `"status":"REFUNDED"`}}, 6},
		{"time", map[string]any{"fromTime": at(10), "toTime": at(20)}, 10},
		{"partition and value", map[string]any{"partitions": []int32{2}, "value": map[string]any{"contains": "REFUNDED"}}, 2},
	} {
		if res := search(topic, s.req); len(res.Messages) != s.want || !res.Complete {
			t.Errorf("search by %s: %d matches, complete %v; want %d", s.name, len(res.Messages), res.Complete, s.want)
		}
	}
	if res := search(topic, map[string]any{"limit": 4}); len(res.Messages) != 4 || res.Complete || res.Stopped != "limit" {
		t.Errorf("search with limit 4: %d matches, complete %v, stopped %q", len(res.Messages), res.Complete, res.Stopped)
	}

	status, raw := c.send("POST", base+"/topics/"+topic+"/export", map[string]any{"header": evenRecords})
	if status != http.StatusOK {
		t.Fatalf("NDJSON export: %d %s", status, raw)
	}
	lines := 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		lines++
		var rec struct {
			Headers []struct {
				Value string `json:"value"`
			} `json:"headers"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil || len(rec.Headers) != 1 || rec.Headers[0].Value != "even" {
			t.Errorf("exported record %s: %v", sc.Text(), err)
		}
	}
	if lines != 15 {
		t.Errorf("the NDJSON export of the even records has %d lines, want 15", lines)
	}
	status, raw = c.send("POST", base+"/topics/"+topic+"/export", map[string]any{"format": "csv", "partitions": []int32{0}})
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if status != http.StatusOK || err != nil || len(rows) != 11 || rows[0][0] != "partition" {
		t.Errorf("CSV export of partition 0: %d, %d rows, %v", status, len(rows), err)
	}

	adm := kadm.NewClient(kc)
	startOffset := func() int64 {
		t.Helper()
		offsets, err := adm.ListStartOffsets(ctx, topic)
		if err != nil {
			t.Fatal(err)
		}
		o, ok := offsets.Lookup(topic, 0)
		if !ok || o.Err != nil {
			t.Fatalf("start offset of partition 0: %v", o.Err)
		}
		return o.Offset
	}
	deleteRecords := func(req map[string]any) deletion {
		t.Helper()
		var res deletion
		c.must(http.StatusOK, "POST", base+"/topics/"+topic+"/delete-records", req, &res)
		return res
	}
	before := []map[string]any{{"partition": 0, "offset": 4}}
	if res := deleteRecords(map[string]any{"partitions": before, "dryRun": true}); !res.DryRun || res.Records != 4 {
		t.Errorf("dry run of deleting 4 records: %+v", res)
	}
	if got := startOffset(); got != 0 {
		t.Errorf("start offset of partition 0 after the dry run = %d, want 0", got)
	}
	if status := c.do("POST", base+"/topics/"+topic+"/delete-records", map[string]any{"partitions": before}, nil); status != http.StatusBadRequest {
		t.Errorf("deleting records without the topic name as confirmation: status %d, want 400", status)
	}
	res := deleteRecords(map[string]any{"partitions": before, "confirm": topic})
	if res.DryRun || res.Records != 4 || len(res.Partitions) != 1 || res.Partitions[0].DeleteBefore != 4 {
		t.Errorf("deleting 4 records: %+v", res)
	}
	if got := startOffset(); got != 4 {
		t.Errorf("start offset of partition 0 after deleting = %d, want 4", got)
	}
	if left := search(topic, map[string]any{"partitions": []int32{0}}); len(left.Messages) != 6 || left.Messages[0].Offset != 4 {
		t.Errorf("partition 0 after deleting: %+v", left.Messages)
	}

	op := c.run(beta, "beta", "copy-messages", copyParams, 3*time.Minute)
	if !strings.Contains(op.Summary, "partitioning same") {
		t.Errorf("copy-messages does not keep the partitions: %s", op.Summary)
	}
	// The even records left: 12, 18 and 24 in partition 0 and five in each of the others.
	copied := search(target, map[string]any{})
	if len(copied.Messages) != 13 || !copied.Complete {
		t.Errorf("copied %d records (complete %v), want 13", len(copied.Messages), copied.Complete)
	}
	for _, m := range copied.Messages {
		var i int
		if _, err := fmt.Sscanf(m.Key, "order-%d", &i); err != nil || i%2 != 0 || int32(i%3) != m.Partition || i < 12 && m.Partition == 0 {
			t.Errorf("unexpected copy of %q in partition %d", m.Key, m.Partition)
		}
	}

	c.must(http.StatusNoContent, "DELETE", "/api/v1/license", nil, nil)
	c.must(http.StatusOK, "GET", "/api/v1/license", nil, &lic)
	if lic.License != nil || lic.licensed(edition.MessageTools) {
		t.Errorf("after removing the key: license %+v, features %+v", lic.License, lic.Features)
	}
	refused("after removing the license")
}
