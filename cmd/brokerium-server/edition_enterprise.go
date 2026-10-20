// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build enterprise

package main

import (
	"context"
	"log/slog"

	"github.com/brokerium/brokerium/ee"
	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// setupEdition adds the features of Brokerium Enterprise.
func setupEdition(ctx context.Context, cfg config.Server, st *store.Store, clusters *cluster.Service, engine *ops.Engine,
	log *slog.Logger) ([]api.Extension, error) {
	return ee.Setup(ctx, ee.Deps{Config: cfg, Store: st, Clusters: clusters, Engine: engine, Log: log})
}
