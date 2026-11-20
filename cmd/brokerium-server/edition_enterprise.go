// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build enterprise

package main

import (
	"context"

	"github.com/brokerium/brokerium/ee"
	"github.com/brokerium/brokerium/internal/api"
)

// setupEdition adds the features of Brokerium Enterprise.
func setupEdition(ctx context.Context, d editionDeps) ([]api.Extension, error) {
	return ee.Setup(ctx, ee.Deps{Config: d.cfg, Store: d.store, Clusters: d.clusters, Engine: d.engine, Box: d.box,
		Sampler: d.sampler, Log: d.log})
}
