// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package strimzi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func kubeconfigFor(server, token string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: lab
clusters:
- name: kind
  cluster:
    server: %s
contexts:
- name: lab
  context: {cluster: kind, user: admin}
- name: other
  context: {cluster: missing, user: admin}
users:
- name: admin
  user:
    token: %s
`, server, token))
}

type fakeAPI struct {
	mu      sync.Mutex
	patches map[string]map[string]any
	auth    string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auth = r.Header.Get("Authorization")
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/apis/kafka.strimzi.io" || r.URL.Path == "/apis/core.strimzi.io":
		_, _ = io.WriteString(w, `{"preferredVersion":{"version":"v1beta2"}}`)
	case r.Method == http.MethodPatch:
		if ct := r.Header.Get("Content-Type"); ct != "application/merge-patch+json" {
			http.Error(w, "bad content type "+ct, http.StatusUnsupportedMediaType)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.patches[r.URL.Path] = body
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	case r.URL.Path == "/apis/kafka.strimzi.io/v1beta2/namespaces/kafka/kafkas/prod":
		_, _ = io.WriteString(w, `{"metadata":{"name":"prod","generation":3},"spec":{"kafka":{"version":"4.3.1"}},
			"status":{"observedGeneration":3,"kafkaVersion":"4.3.1","kafkaMetadataVersion":"4.3-IV0",
			"conditions":[{"type":"Ready","status":"True"}]}}`)
	case r.URL.Path == "/apis/kafka.strimzi.io/v1beta2/namespaces/kafka/kafkanodepools":
		if r.URL.Query().Get("labelSelector") != "strimzi.io/cluster=prod" {
			http.Error(w, "missing label selector", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"items":[
			{"metadata":{"name":"controllers","generation":1},"spec":{"replicas":3,"roles":["controller"]},
			 "status":{"nodeIds":[0,1,2],"replicas":3,"observedGeneration":1}},
			{"metadata":{"name":"brokers","generation":2},"spec":{"replicas":3,"roles":["broker"]},
			 "status":{"nodeIds":[100,101,102],"replicas":3,"observedGeneration":2}}]}`)
	default:
		http.NotFound(w, r)
	}
}

func TestClusterAgainstFakeAPI(t *testing.T) {
	t.Parallel()
	api := &fakeAPI{patches: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	kube, err := FromKubeconfig(kubeconfigFor(srv.URL, "s3cret"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Open(ctx, kube, "kafka", "missing"); err == nil {
		t.Error("opening a missing Kafka resource succeeded")
	}
	c, err := Open(ctx, kube, "kafka", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if api.auth != "Bearer s3cret" {
		t.Errorf("Authorization = %q", api.auth)
	}
	k, err := c.Kafka(ctx)
	if err != nil || !k.Ready() || k.Status.KafkaMetadataVersion != "4.3-IV0" {
		t.Fatalf("Kafka = %+v, %v", k, err)
	}
	pools, err := c.NodePools(ctx)
	if err != nil || len(pools) != 2 {
		t.Fatalf("NodePools = %+v, %v", pools, err)
	}
	if pools[0].Metadata.Name != "brokers" || !pools[0].Brokers() || pools[0].Controllers() || !pools[1].Controllers() || !pools[0].Settled() {
		t.Errorf("pool roles or order wrong: %+v", pools)
	}

	if err := c.ScalePool(ctx, "brokers", 2, []int32{102}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"spec": map[string]any{"replicas": float64(2)},
		"metadata": map[string]any{"annotations": map[string]any{AnnotationRemoveNodeIDs: "[102]"}}}
	if got := api.patches["/apis/kafka.strimzi.io/v1beta2/namespaces/kafka/kafkanodepools/brokers"]; !reflect.DeepEqual(got, want) {
		t.Errorf("scale patch = %v, want %v", got, want)
	}
	if err := c.SetVersion(ctx, "4.4.0", "4.3-IV0"); err != nil {
		t.Fatal(err)
	}
	if got := api.patches["/apis/kafka.strimzi.io/v1beta2/namespaces/kafka/kafkas/prod"]; !reflect.DeepEqual(got,
		map[string]any{"spec": map[string]any{"kafka": map[string]any{"version": "4.4.0", "metadataVersion": "4.3-IV0"}}}) {
		t.Errorf("version patch = %v", got)
	}
	if err := c.Roll(ctx, "brokers"); err != nil {
		t.Fatal(err)
	}
	if _, ok := api.patches["/apis/core.strimzi.io/v1beta2/namespaces/kafka/strimzipodsets/prod-brokers"]; !ok {
		t.Error("roll did not annotate the StrimziPodSet")
	}
}

func TestFromKubeconfigErrors(t *testing.T) {
	t.Parallel()
	if _, err := FromKubeconfig(kubeconfigFor("https://k8s:6443", "t"), "other"); err == nil || !strings.Contains(err.Error(), "no cluster") {
		t.Errorf("missing cluster: %v", err)
	}
	if _, err := FromKubeconfig(kubeconfigFor("https://k8s:6443", "t"), "nope"); err == nil {
		t.Error("missing context accepted")
	}
	execOnly := strings.Replace(string(kubeconfigFor("https://k8s:6443", "")), "token: ", "exec: {command: aws}\n    x: ", 1)
	if _, err := FromKubeconfig([]byte(execOnly), ""); err == nil || !strings.Contains(err.Error(), "exec plugins") {
		t.Errorf("exec-only user: %v", err)
	}
	if _, err := FromKubeconfig([]byte("not: [yaml"), ""); err == nil {
		t.Error("broken YAML accepted")
	}
}

func TestReadiness(t *testing.T) {
	t.Parallel()
	k := &Kafka{}
	k.Metadata.Generation = 4
	k.Status.ObservedGeneration = 3
	k.Status.Conditions = []Condition{{Type: "Ready", Status: "True"}}
	if k.Ready() {
		t.Error("a Kafka with an unobserved generation is ready")
	}
	k.Status.ObservedGeneration = 4
	if !k.Ready() {
		t.Error("observed and Ready=True is not ready")
	}
	k.Status.Conditions = []Condition{{Type: "NotReady", Status: "True", Reason: "Timeout", Message: "pods pending"}}
	if k.Ready() || k.Problem() != "Timeout: pods pending" {
		t.Errorf("NotReady handling: ready=%v problem=%q", k.Ready(), k.Problem())
	}

	var p NodePool
	p.Metadata.Generation, p.Status.ObservedGeneration = 2, 2
	p.Spec.Replicas, p.Status.Replicas, p.Status.NodeIDs = 4, 3, []int32{1, 2, 3}
	if p.Settled() {
		t.Error("a pool with fewer replicas than requested is settled")
	}
}

func TestIDHelpers(t *testing.T) {
	t.Parallel()
	if got := HighestIDs([]int32{100, 103, 101, 102}, 2); !reflect.DeepEqual(got, []int32{102, 103}) {
		t.Errorf("HighestIDs = %v", got)
	}
	if got := HighestIDs([]int32{1}, 3); !reflect.DeepEqual(got, []int32{1}) {
		t.Errorf("HighestIDs beyond length = %v", got)
	}
	if IDList([]int32{3, 4}) != "[3, 4]" || IDList(nil) != "[]" {
		t.Error("IDList")
	}
}
