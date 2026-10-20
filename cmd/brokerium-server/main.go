// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command brokerium-server runs the Brokerium control plane: API, dashboard, operations engine
// and agent gateway.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brokerium/brokerium/internal/agentgw"
	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/metrics"
	"github.com/brokerium/brokerium/internal/nodes"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/secrets"
	"github.com/brokerium/brokerium/internal/store"
	"github.com/brokerium/brokerium/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.String())
		return
	}
	level := slog.LevelInfo
	if os.Getenv("BROKERIUM_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if err := run(log); err != nil {
		log.Error("brokerium-server stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadServer()
	if err != nil {
		return err
	}
	log.Info("starting brokerium-server", "version", version.String(), "http", cfg.HTTPAddr)

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	generated, err := api.EnsureAdmin(ctx, st, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("creating admin user: %w", err)
	}
	if generated != "" {
		log.Warn("created initial administrator; change the password after the first login",
			"username", cfg.AdminUsername, "password", generated)
	}

	box, err := secrets.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	certWarning := agentgw.WarningPeriod(cfg.AgentCertLifetime)
	clusters := cluster.New(st, box, log)
	clusters.CertificateWarning = certWarning
	sampler := metrics.NewSampler(5 * time.Minute)
	clusters.OnRefresh(func(v *cluster.View) {
		if v.Snapshot != nil {
			sampler.Observe(v.Cluster.ID, v.Snapshot)
		}
	})
	engine := ops.New(st, clusters, sampler, log)

	ca, err := agentgw.LoadOrCreateCA(ctx, st, box)
	if err != nil {
		return fmt.Errorf("loading agent CA: %w (if the database was created by a server with another secret key, "+
			"set BROKERIUM_SECRET_KEY to that key or keep BROKERIUM_DATA_DIR on a persistent volume)", err)
	}
	gateway := agentgw.New(st, ca, box, log)
	gateway.CertLifetime = cfg.AgentCertLifetime
	dockerClient, err := docker.New(cfg.DockerHost)
	if err != nil {
		log.Info("docker provider disabled", "reason", err)
		dockerClient = nil
	}
	nodeManager := &nodes.Manager{Store: st, Gateway: gateway, Docker: dockerClient, Clusters: clusters, Log: log,
		Cfg: nodes.Config{AgentEndpoint: cfg.AgentEndpoint, AgentImage: cfg.AgentImage, LabNetwork: cfg.LabNetwork}}
	engine.Extend(nodeManager)

	go clusters.Run(ctx, cfg.SnapshotInterval)
	go engine.Run(ctx, cfg.WorkerInterval)
	errCh := make(chan error, 3)
	go func() { errCh <- gateway.Serve(ctx, cfg.GRPCAddr, cfg.AgentServerNames) }()
	if cfg.AgentInsecureAddr != "" {
		go func() { errCh <- gateway.ServeInsecure(ctx, cfg.AgentInsecureAddr) }()
	}

	srv := &api.Server{Store: st, Clusters: clusters, Box: box, Log: log, SecureCookies: cfg.SecureCookies}
	httpServer := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: srv.Handler(
			&api.OperationsAPI{Engine: engine, Sampler: sampler},
			&api.NodesAPI{Gateway: gateway, Pools: nodeManager, PublicURL: cfg.PublicURL, AgentEndpoint: cfg.AgentEndpoint,
				Proxy: cfg.AgentProxy, NoProxy: cfg.AgentNoProxy, CertificateWarning: certWarning,
				Transport: cfg.AgentTransport, InsecureEndpoint: cfg.AgentInsecureEndpoint, InsecureTLS: cfg.AgentInsecureTLS},
		),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { errCh <- httpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
