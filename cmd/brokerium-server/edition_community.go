// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build !enterprise

package main

import (
	"context"

	"github.com/brokerium/brokerium/internal/api"
)

// setupEdition adds the features of the edition. The Community edition has none.
func setupEdition(_ context.Context, d editionDeps) ([]api.Extension, error) {
	if d.cfg.License != "" || d.cfg.LicenseFile != "" {
		d.log.Warn("BROKERIUM_LICENSE and BROKERIUM_LICENSE_FILE are ignored by the Community edition; " +
			"license keys need the brokerium-server-enterprise image or binary")
	}
	if d.cfg.Prometheus.Set() || d.cfg.MetricsToken != "" || d.cfg.MetricsTokenFile != "" {
		d.log.Warn("BROKERIUM_PROMETHEUS_* and BROKERIUM_METRICS_TOKEN* are ignored by the Community edition; " +
			"monitoring is a feature of Brokerium Enterprise")
	}
	return nil, nil
}
