// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package strimzi

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Annotations understood by the Strimzi operator.
const (
	AnnotationRemoveNodeIDs = "strimzi.io/remove-node-ids"
	AnnotationManualRolling = "strimzi.io/manual-rolling-update"
)

const (
	labelCluster            = "strimzi.io/cluster"
	kafkaGroup              = "kafka.strimzi.io"
	coreGroup               = "core.strimzi.io"
	defaultVersion          = "v1beta2"
	conditionReady          = "Ready"
	conditionNotReady       = "NotReady"
	statusTrue              = "True"
	roleBroker, roleControl = "broker", "controller"
	resourcePath            = "/apis/%s/%s/namespaces/%s/%s"
	kafkasResource          = "kafkas"
	nodePoolsResource       = "kafkanodepools"
	podSetsResource         = "strimzipodsets"
)

// Condition is a status condition of a Strimzi resource.
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type meta struct {
	Name        string            `json:"name"`
	Generation  int64             `json:"generation"`
	Annotations map[string]string `json:"annotations"`
}

// NodePool is a KafkaNodePool.
type NodePool struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Replicas int32    `json:"replicas"`
		Roles    []string `json:"roles"`
	} `json:"spec"`
	Status struct {
		NodeIDs            []int32     `json:"nodeIds"`
		Replicas           int32       `json:"replicas"`
		Roles              []string    `json:"roles"`
		ObservedGeneration int64       `json:"observedGeneration"`
		Conditions         []Condition `json:"conditions"`
	} `json:"status"`
}

// Brokers reports whether the pool runs brokers.
func (p NodePool) Brokers() bool { return hasRole(p.Spec.Roles, roleBroker) }

// Controllers reports whether the pool runs controllers.
func (p NodePool) Controllers() bool { return hasRole(p.Spec.Roles, roleControl) }

// Settled reports whether the operator has processed the latest spec of the pool.
func (p NodePool) Settled() bool {
	return p.Status.ObservedGeneration >= p.Metadata.Generation && p.Status.Replicas == p.Spec.Replicas &&
		len(p.Status.NodeIDs) == int(p.Spec.Replicas)
}

// Kafka is the Kafka custom resource.
type Kafka struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Kafka struct {
			Version         string `json:"version"`
			MetadataVersion string `json:"metadataVersion"`
		} `json:"kafka"`
	} `json:"spec"`
	Status struct {
		Conditions           []Condition `json:"conditions"`
		ObservedGeneration   int64       `json:"observedGeneration"`
		ClusterID            string      `json:"clusterId"`
		KafkaVersion         string      `json:"kafkaVersion"`
		KafkaMetadataVersion string      `json:"kafkaMetadataVersion"`
	} `json:"status"`
}

// Ready reports whether the operator reconciled the latest spec and the cluster is ready.
func (k *Kafka) Ready() bool {
	if k.Status.ObservedGeneration < k.Metadata.Generation {
		return false
	}
	for _, c := range k.Status.Conditions {
		if c.Type == conditionReady {
			return c.Status == statusTrue
		}
	}
	return false
}

// Problem returns the message of a NotReady condition, if any.
func (k *Kafka) Problem() string {
	for _, c := range k.Status.Conditions {
		if c.Type == conditionNotReady && c.Status == statusTrue {
			return strings.TrimSpace(c.Reason + ": " + c.Message)
		}
	}
	return ""
}

// Cluster is one Strimzi-managed Kafka cluster.
type Cluster struct {
	Kube        *Kube
	Namespace   string
	Name        string
	kafkaAPI    string
	podSetsAPI  string
	nodePoolAPI string
}

// Open checks that the Kafka resource exists and picks the served Strimzi API versions.
func Open(ctx context.Context, kube *Kube, namespace, name string) (*Cluster, error) {
	if namespace == "" || name == "" {
		return nil, fmt.Errorf("namespace and Kafka name are required")
	}
	kafkaVersion, err := preferredVersion(ctx, kube, kafkaGroup)
	if err != nil {
		return nil, fmt.Errorf("strimzi CRDs are not installed: %w", err)
	}
	coreVersion, err := preferredVersion(ctx, kube, coreGroup)
	if err != nil {
		return nil, fmt.Errorf("strimzi CRDs are not installed: %w", err)
	}
	c := &Cluster{Kube: kube, Namespace: namespace, Name: name,
		kafkaAPI: kafkaGroup + "/" + kafkaVersion, nodePoolAPI: kafkaGroup + "/" + kafkaVersion, podSetsAPI: coreGroup + "/" + coreVersion}
	if _, err := c.Kafka(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func preferredVersion(ctx context.Context, kube *Kube, group string) (string, error) {
	var apiGroup struct {
		PreferredVersion struct {
			Version string `json:"version"`
		} `json:"preferredVersion"`
	}
	if err := kube.Get(ctx, "/apis/"+group, &apiGroup); err != nil {
		return "", err
	}
	if v := apiGroup.PreferredVersion.Version; v != "" {
		return v, nil
	}
	return defaultVersion, nil
}

func (c *Cluster) path(api, resource, name string) string {
	groupName, version, _ := strings.Cut(api, "/")
	p := fmt.Sprintf(resourcePath, groupName, version, url.PathEscape(c.Namespace), resource)
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

// Kafka reads the Kafka resource.
func (c *Cluster) Kafka(ctx context.Context) (*Kafka, error) {
	var k Kafka
	if err := c.Kube.Get(ctx, c.path(c.kafkaAPI, kafkasResource, c.Name), &k); err != nil {
		return nil, fmt.Errorf("reading Kafka %s/%s: %w", c.Namespace, c.Name, err)
	}
	return &k, nil
}

// NodePools lists the node pools of the cluster, sorted by name.
func (c *Cluster) NodePools(ctx context.Context) ([]NodePool, error) {
	var list struct {
		Items []NodePool `json:"items"`
	}
	path := c.path(c.nodePoolAPI, nodePoolsResource, "") + "?labelSelector=" + url.QueryEscape(labelCluster+"="+c.Name)
	if err := c.Kube.Get(ctx, path, &list); err != nil {
		return nil, fmt.Errorf("listing node pools: %w", err)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Metadata.Name < list.Items[j].Metadata.Name })
	return list.Items, nil
}

// NodePool reads one node pool.
func (c *Cluster) NodePool(ctx context.Context, name string) (*NodePool, error) {
	var p NodePool
	if err := c.Kube.Get(ctx, c.path(c.nodePoolAPI, nodePoolsResource, name), &p); err != nil {
		return nil, fmt.Errorf("reading node pool %s: %w", name, err)
	}
	return &p, nil
}

// ScalePool sets the replicas of a node pool. When remove is not empty, Strimzi removes
// exactly these node ids instead of the highest ones.
func (c *Cluster) ScalePool(ctx context.Context, pool string, replicas int32, remove []int32) error {
	patch := map[string]any{"spec": map[string]any{"replicas": replicas}}
	if len(remove) > 0 {
		patch["metadata"] = map[string]any{"annotations": map[string]any{AnnotationRemoveNodeIDs: IDList(remove)}}
	}
	return c.Kube.MergePatch(ctx, c.path(c.nodePoolAPI, nodePoolsResource, pool), patch, nil)
}

// SetVersion sets spec.kafka.version and pins metadataVersion, so the operator does not raise
// metadata.version on its own and the upgrade can still be rolled back.
func (c *Cluster) SetVersion(ctx context.Context, version, metadataVersion string) error {
	kafka := map[string]any{"version": version}
	if metadataVersion != "" {
		kafka["metadataVersion"] = metadataVersion
	}
	return c.Kube.MergePatch(ctx, c.path(c.kafkaAPI, kafkasResource, c.Name), map[string]any{"spec": map[string]any{"kafka": kafka}}, nil)
}

// SetMetadataVersion finalizes an upgrade through the operator.
func (c *Cluster) SetMetadataVersion(ctx context.Context, metadataVersion string) error {
	return c.Kube.MergePatch(ctx, c.path(c.kafkaAPI, kafkasResource, c.Name),
		map[string]any{"spec": map[string]any{"kafka": map[string]any{"metadataVersion": metadataVersion}}}, nil)
}

// Roll asks the operator to restart every pod of a node pool, one at a time.
func (c *Cluster) Roll(ctx context.Context, pool string) error {
	return c.Kube.MergePatch(ctx, c.path(c.podSetsAPI, podSetsResource, c.Name+"-"+pool),
		map[string]any{"metadata": map[string]any{"annotations": map[string]any{AnnotationManualRolling: "true"}}}, nil)
}

// RollPending reports whether the operator has not finished a requested roll of a pool yet;
// it removes the annotation once all pods were restarted.
func (c *Cluster) RollPending(ctx context.Context, pool string) (bool, error) {
	var ps struct {
		Metadata meta `json:"metadata"`
	}
	if err := c.Kube.Get(ctx, c.path(c.podSetsAPI, podSetsResource, c.Name+"-"+pool), &ps); err != nil {
		return false, err
	}
	return ps.Metadata.Annotations[AnnotationManualRolling] == "true", nil
}

// IDList renders node ids the way Strimzi annotations expect them: "[3, 4]".
func IDList(ids []int32) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(int(id))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// HighestIDs returns the n highest node ids of a pool, which Strimzi removes by default.
func HighestIDs(ids []int32, n int) []int32 {
	sorted := append([]int32(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
	if n > len(sorted) {
		n = len(sorted)
	}
	out := sorted[:n]
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
