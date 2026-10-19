// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/strimzi"
	"github.com/brokerium/brokerium/internal/store"
)

// Step kinds of Strimzi clusters.
const (
	KindStrimziScale    = "strimzi-scale"
	KindStrimziWaitPool = "strimzi-wait-pool"
	KindStrimziRoll     = "strimzi-roll"
	KindStrimziVersion  = "strimzi-set-version"
	KindStrimziMetadata = "strimzi-set-metadata"
)

// StrimziSettings locate the Kafka resource of a Strimzi cluster (clusters.settings).
type StrimziSettings struct {
	Namespace   string `json:"namespace"`
	Kafka       string `json:"kafka"`
	KubeContext string `json:"kubeContext"`
	// InCluster uses the service account of the Brokerium pod instead of a kubeconfig.
	InCluster bool `json:"inCluster"`
}

// StrimziArgs are the arguments of Strimzi steps.
type StrimziArgs struct {
	Pool            string  `json:"pool,omitempty"`
	Replicas        int32   `json:"replicas,omitempty"`
	Remove          []int32 `json:"remove,omitempty"`
	Version         string  `json:"version,omitempty"`
	MetadataVersion string  `json:"metadataVersion,omitempty"`
}

func strimziSettingsOf(c *store.Cluster) StrimziSettings {
	var s StrimziSettings
	raw, _ := json.Marshal(c.Settings)
	_ = json.Unmarshal(raw, &s)
	return s
}

// strimziCluster connects to the Kubernetes API of a Strimzi cluster.
func (m *Manager) strimziCluster(ctx context.Context, c *store.Cluster) (*strimzi.Cluster, error) {
	s := strimziSettingsOf(c)
	var kube *strimzi.Kube
	var err error
	if s.InCluster {
		kube, err = strimzi.InCluster()
	} else {
		p, perr := m.Clusters.Profile(c)
		switch {
		case perr != nil:
			err = perr
		case p.Secrets.Kubeconfig == "":
			return nil, errors.New("the cluster has no kubeconfig; add one in the cluster settings or enable in-cluster access")
		default:
			kube, err = strimzi.FromKubeconfig([]byte(p.Secrets.Kubeconfig), s.KubeContext)
		}
	}
	if err != nil {
		return nil, err
	}
	return strimzi.Open(ctx, kube, s.Namespace, s.Kafka)
}

// NodePools lists the node pools of a Strimzi cluster.
func (m *Manager) NodePools(ctx context.Context, c *store.Cluster) ([]strimzi.NodePool, error) {
	sc, err := m.strimziCluster(ctx, c)
	if err != nil {
		return nil, err
	}
	return sc.NodePools(ctx)
}

type scalePoolParams struct {
	Pool     string `json:"pool"`
	Replicas *int32 `json:"replicas"`
}

func (m *Manager) planScalePool(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[scalePoolParams](raw)
	if err != nil {
		return nil, err
	}
	if params.Pool == "" || params.Replicas == nil || *params.Replicas < 0 {
		return nil, errors.New("pool and replicas are required")
	}
	sc, err := m.strimziCluster(ctx, p.Cluster)
	if err != nil {
		return nil, err
	}
	pool, err := sc.NodePool(ctx, params.Pool)
	if err != nil {
		return nil, err
	}
	if pool.Controllers() {
		return nil, errors.New("pools with the controller role are not scaled: Strimzi does not support dynamic controller quorums yet")
	}
	if !pool.Settled() {
		return nil, fmt.Errorf("node pool %s is still being reconciled by the operator", pool.Metadata.Name)
	}
	cur, want := pool.Spec.Replicas, *params.Replicas
	gate := ops.StepSpec{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}
	switch {
	case want == cur:
		return nil, fmt.Errorf("node pool %s already has %d replicas", pool.Metadata.Name, cur)
	case want > cur:
		steps := []ops.StepSpec{gate,
			{Name: fmt.Sprintf("Scale node pool %s to %d", pool.Metadata.Name, want), Kind: KindStrimziScale,
				Args: StrimziArgs{Pool: pool.Metadata.Name, Replicas: want}},
			{Name: "Wait for the new brokers", Kind: KindStrimziWaitPool, Args: StrimziArgs{Pool: pool.Metadata.Name}},
			{Name: "Rebalance onto the new brokers", Kind: KindRebalance, Args: NodeArgs{Mode: "even"}},
		}
		return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf("Scale node pool %s from %d to %d brokers and rebalance",
			pool.Metadata.Name, cur, want)}, Steps: steps}, nil
	}
	remove := strimzi.HighestIDs(pool.Status.NodeIDs, int(cur-want))
	move, plan, err := ops.EvacuateSteps(p, remove)
	if err != nil {
		return nil, err
	}
	steps := append(move,
		ops.StepSpec{Name: fmt.Sprintf("Scale node pool %s to %d, removing %s", pool.Metadata.Name, want, strimzi.IDList(remove)),
			Kind: KindStrimziScale, Args: StrimziArgs{Pool: pool.Metadata.Name, Replicas: want, Remove: remove}},
		ops.StepSpec{Name: "Wait for the operator to remove the brokers", Kind: KindStrimziWaitPool,
			Args: StrimziArgs{Pool: pool.Metadata.Name, Remove: remove}},
	)
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Scale node pool %s from %d to %d: move %d partition(s) off brokers %s first",
		pool.Metadata.Name, cur, want, len(plan.Moves), strimzi.IDList(remove)), Moves: plan.Moves, Before: &plan.Before,
		After: &plan.After, DataToMoveBytes: plan.DataToMoveBytes, Warnings: plan.Warnings}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

func (m *Manager) planStrimziRestart(ctx context.Context, p *ops.Planner) (*ops.Planned, error) {
	sc, err := m.strimziCluster(ctx, p.Cluster)
	if err != nil {
		return nil, err
	}
	pools, err := sc.NodePools(ctx)
	if err != nil {
		return nil, err
	}
	// Controllers first, like for agent-managed clusters.
	slices.SortStableFunc(pools, func(a, b strimzi.NodePool) int {
		switch {
		case a.Controllers() && !b.Controllers():
			return -1
		case b.Controllers() && !a.Controllers():
			return 1
		}
		return 0
	})
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	var names []string
	for _, pool := range pools {
		names = append(names, pool.Metadata.Name)
		steps = append(steps,
			ops.StepSpec{Name: "Roll node pool " + pool.Metadata.Name, Kind: KindStrimziRoll, Args: StrimziArgs{Pool: pool.Metadata.Name}},
			ops.StepSpec{Name: "Check health after " + pool.Metadata.Name, Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}})
	}
	return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf(
		"Ask Strimzi to restart the pods of node pools %s one pool at a time", strings.Join(names, ", "))}, Steps: steps}, nil
}

func (m *Manager) planStrimziUpgrade(ctx context.Context, p *ops.Planner, version string) (*ops.Planned, error) {
	sc, err := m.strimziCluster(ctx, p.Cluster)
	if err != nil {
		return nil, err
	}
	k, err := sc.Kafka(ctx)
	if err != nil {
		return nil, err
	}
	current := k.Status.KafkaVersion
	if current != "" && compareVersions(version, current) <= 0 {
		return nil, fmt.Errorf("target %s is not newer than the running version %s", version, current)
	}
	pin := k.Spec.Kafka.MetadataVersion
	if pin == "" {
		pin = k.Status.KafkaMetadataVersion
	}
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Set the Kafka version of %s/%s to %s; Strimzi rolls the pods one by one",
		sc.Namespace, sc.Name, version), Warnings: []string{}}
	for _, bug := range issuesOf(version) {
		doc.Warnings = append(doc.Warnings, "target "+version+" has "+bug+"; prefer 4.3.2 or later")
	}
	if pin != "" {
		doc.Warnings = append(doc.Warnings, "metadataVersion stays pinned at "+pin+" until you run finalize-upgrade")
	}
	steps := []ops.StepSpec{
		{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}},
		{Name: "Set spec.kafka.version to " + version, Kind: KindStrimziVersion, Args: StrimziArgs{Version: version, MetadataVersion: pin}},
		{Name: "Check health on the new version", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}},
		{Name: "Elect preferred leaders", Kind: ops.KindElectPreferred, Args: ops.ReassignArgs{}},
	}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

func (m *Manager) planFinalize(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	if p.Cluster.Provider != store.ProviderStrimzi {
		return ops.PlanFinalize(ctx, p, raw)
	}
	var target string
	for _, f := range p.View.Snapshot.Features {
		if f.Name == "metadata.version" && f.Finalized < f.SupportedMax {
			target = cluster.MetadataVersionName(f.SupportedMax)
		}
	}
	if target == "" {
		return nil, errors.New("metadata.version is already at the highest level the nodes support")
	}
	doc := ops.PlanDoc{Irreversible: true, Summary: "Set spec.kafka.metadataVersion to " + target + " through Strimzi",
		Warnings: []string{"metadata.version cannot be lowered again"}}
	return &ops.Planned{Doc: doc, Steps: []ops.StepSpec{
		{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}},
		{Name: "Set metadataVersion to " + target, Kind: KindStrimziMetadata, Args: StrimziArgs{MetadataVersion: target}},
	}}, nil
}

func strimziArgs(raw json.RawMessage) (StrimziArgs, error) {
	var a StrimziArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("invalid step arguments: %w", err)
	}
	return a, nil
}

func (m *Manager) stepStrimziScale(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := strimziArgs(raw)
	if err != nil {
		return nil, err
	}
	sc, err := m.strimziCluster(ctx, x.Cluster)
	if err != nil {
		return nil, err
	}
	if err := sc.ScalePool(ctx, a.Pool, a.Replicas, a.Remove); err != nil {
		return nil, err
	}
	return map[string]any{"pool": a.Pool, "replicas": a.Replicas, "remove": a.Remove}, nil
}

// stepStrimziWaitPool waits until the operator settled the pool and the brokers it added are
// serving, or the brokers it removed are gone from the cluster.
func (m *Manager) stepStrimziWaitPool(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := strimziArgs(raw)
	if err != nil {
		return nil, err
	}
	sc, err := m.strimziCluster(ctx, x.Cluster)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, 20*time.Minute, 5*time.Second, "node pool "+a.Pool, func(ctx context.Context) (bool, any, error) {
		pool, err := sc.NodePool(ctx, a.Pool)
		if err != nil {
			return false, nil, err
		}
		if !pool.Settled() {
			return false, map[string]any{"replicas": pool.Status.Replicas, "want": pool.Spec.Replicas}, nil
		}
		v, err := x.View(ctx)
		if err != nil {
			return false, nil, nil
		}
		for _, id := range a.Remove {
			if slices.ContainsFunc(v.View.Snapshot.Brokers, func(b kafka.Broker) bool { return b.ID == id }) {
				return false, map[string]any{"waitingForRemoval": id}, nil
			}
		}
		if pool.Brokers() {
			if ok, detail, err := brokersServing(ctx, v.View.Snapshot, pool.Status.NodeIDs, x.Client.PingBroker); !ok {
				return false, detail, err
			}
		}
		return true, map[string]any{"nodeIds": pool.Status.NodeIDs}, nil
	})
}

// brokersServing reports whether every broker in ids is alive, unfenced, has its log dirs in the
// snapshot and answers on its advertised listener. A broker registers before its listener is
// reachable from outside Kubernetes, and a rebalance needs requests to every broker.
func brokersServing(ctx context.Context, snap *kafka.Snapshot, ids []int32,
	ping func(context.Context, int32) error) (bool, any, error) {
	byID := map[int32]kafka.Broker{}
	logDirsKnown := false
	for _, b := range snap.Brokers {
		byID[b.ID] = b
		logDirsKnown = logDirsKnown || len(b.LogDirs) > 0
	}
	for _, id := range ids {
		b, ok := byID[id]
		switch {
		case !ok || !b.Alive || b.Fenced:
			return false, map[string]any{"waitingForBroker": id}, nil
		case logDirsKnown && len(b.LogDirs) == 0:
			return false, map[string]any{"waitingForLogDirs": id}, nil
		}
		if err := ping(ctx, id); err != nil {
			return false, map[string]any{"waitingForListener": id}, err
		}
	}
	return true, nil, nil
}

func (m *Manager) stepStrimziRoll(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := strimziArgs(raw)
	if err != nil {
		return nil, err
	}
	sc, err := m.strimziCluster(ctx, x.Cluster)
	if err != nil {
		return nil, err
	}
	if err := sc.Roll(ctx, a.Pool); err != nil {
		return nil, err
	}
	return poll(ctx, x, 30*time.Minute, 5*time.Second, "the roll of "+a.Pool, func(ctx context.Context) (bool, any, error) {
		pending, err := sc.RollPending(ctx, a.Pool)
		if err != nil || pending {
			return false, map[string]any{"pending": pending}, nil
		}
		k, err := sc.Kafka(ctx)
		if err != nil {
			return false, nil, nil
		}
		return k.Ready(), map[string]any{"ready": k.Ready(), "problem": k.Problem()}, nil
	})
}

func (m *Manager) stepStrimziVersion(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := strimziArgs(raw)
	if err != nil {
		return nil, err
	}
	sc, err := m.strimziCluster(ctx, x.Cluster)
	if err != nil {
		return nil, err
	}
	if err := sc.SetVersion(ctx, a.Version, a.MetadataVersion); err != nil {
		return nil, err
	}
	return poll(ctx, x, 60*time.Minute, 10*time.Second, "Kafka "+a.Version, func(ctx context.Context) (bool, any, error) {
		k, err := sc.Kafka(ctx)
		if err != nil {
			return false, nil, nil
		}
		done := k.Ready() && k.Status.KafkaVersion == a.Version
		return done, map[string]any{"kafkaVersion": k.Status.KafkaVersion, "ready": k.Ready(), "problem": k.Problem()}, nil
	})
}

func (m *Manager) stepStrimziMetadata(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := strimziArgs(raw)
	if err != nil {
		return nil, err
	}
	sc, err := m.strimziCluster(ctx, x.Cluster)
	if err != nil {
		return nil, err
	}
	if err := sc.SetMetadataVersion(ctx, a.MetadataVersion); err != nil {
		return nil, err
	}
	return poll(ctx, x, 20*time.Minute, 5*time.Second, "metadataVersion "+a.MetadataVersion, func(ctx context.Context) (bool, any, error) {
		k, err := sc.Kafka(ctx)
		if err != nil {
			return false, nil, nil
		}
		done := k.Ready() && k.Status.KafkaMetadataVersion == a.MetadataVersion
		return done, map[string]any{"metadataVersion": k.Status.KafkaMetadataVersion, "ready": k.Ready()}, nil
	})
}
