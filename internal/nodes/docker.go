// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"

	"github.com/brokerium/brokerium/internal/store"
)

// CheckPlan refuses steps that act on the nodes of a Docker cluster while the server does not
// offer Docker clusters (ADR 0019). Kafka-level operations on such clusters stay available.
func (m *Manager) CheckPlan(ctx context.Context, c *store.Cluster, kinds []string) error {
	if c.Provider != store.ProviderDocker {
		return nil
	}
	nodeSteps := m.StepKinds()
	for _, k := range kinds {
		if _, ok := nodeSteps[k]; ok {
			return m.DockerClusters.Check(ctx).Err()
		}
	}
	return nil
}
