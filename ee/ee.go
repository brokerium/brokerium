// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package ee adds the features of Brokerium Enterprise to the server (ADR 0016).
package ee

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brokerium/brokerium/ee/licensing"
	"github.com/brokerium/brokerium/ee/messagetools"
	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// Deps are the parts of the server that Enterprise features build on.
type Deps struct {
	Config   config.Server
	Store    *store.Store
	Clusters *cluster.Service
	Engine   *ops.Engine
	Log      *slog.Logger
}

// evaluateEvery is how often the state of the license is evaluated again.
const evaluateEvery = time.Hour

// Setup loads the license key, installs its manager as the license manager of the edition,
// starts the hourly evaluation and returns the API routes of the Enterprise features.
// Enterprise operation types set ops.OpType.Feature and are added with ops.Engine.Register.
func Setup(ctx context.Context, d Deps) ([]api.Extension, error) {
	if licensing.TrustsTestKey() {
		d.Log.Warn("this build trusts the public test signing key (licensetest); never run it in production")
	}
	m := &licensing.Manager{Text: d.Config.License, File: d.Config.LicenseFile, Store: d.Store, Log: d.Log}
	if err := m.Load(ctx); err != nil {
		return nil, fmt.Errorf("loading the license key: %w", err)
	}
	edition.SetManager(m)
	go m.Run(ctx, evaluateEvery)
	return []api.Extension{featureRoutes{feature: edition.MessageTools, routes: messagetools.Routes}}, nil
}

// featureRoutes registers the routes of an Enterprise feature behind its license check.
type featureRoutes struct {
	feature string
	routes  func(s *api.Server) []messagetools.Route
}

func (f featureRoutes) Register(s *api.Server) {
	for _, r := range f.routes(s) {
		s.Route(r.Pattern, r.Role, RequireFeature(f.feature, r.Handler))
	}
}

// RequireFeature wraps the handler of an Enterprise feature: unless the license covers the
// feature, it answers 403 license_required.
func RequireFeature(feature string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !edition.Allows(feature) {
			api.WriteError(w, http.StatusForbidden, "license_required", edition.LicenseRequired(feature).Error())
			return
		}
		h(w, r)
	}
}
