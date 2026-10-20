// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build !enterprise

package main

import (
	"context"
	"log/slog"

	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// setupEdition adds the features of the edition. The Community edition has none.
func setupEdition(_ context.Context, cfg config.Server, _ *store.Store, _ *cluster.Service, _ *ops.Engine,
	log *slog.Logger) ([]api.Extension, error) {
	if cfg.License != "" || cfg.LicenseFile != "" {
		log.Warn("BROKERIUM_LICENSE and BROKERIUM_LICENSE_FILE are ignored by the Community edition; " +
			"license keys need the brokerium-server-enterprise image or binary")
	}
	return nil, nil
}
