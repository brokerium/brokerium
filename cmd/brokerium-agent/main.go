// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command brokerium-agent runs on Kafka hosts (or as the main process of lab node containers),
// connects out to brokerium-server and executes typed lifecycle commands (ADR 0009).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/brokerium/brokerium/internal/agent"
	"github.com/brokerium/brokerium/internal/version"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Println(`brokerium-agent manages Apache Kafka on this host for brokerium-server.

Usage:
  brokerium-agent run [flags]   enroll (first run) and serve commands
  brokerium-agent version

Flags (or BROKERIUM_AGENT_* environment variables):`)
		fs, _ := flags()
		fs.SetOutput(os.Stdout)
		fs.PrintDefaults()
		return
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version.String())
	case "run":
		fs, cfg := flags()
		_ = fs.Parse(os.Args[2:])
		if err := run(*cfg); err != nil {
			fmt.Fprintln(os.Stderr, "brokerium-agent:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q; run brokerium-agent help\n", os.Args[1])
		os.Exit(2)
	}
}

func flags() (*flag.FlagSet, *agent.Config) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfg := &agent.Config{}
	fs.StringVar(&cfg.Server, "server", env("BROKERIUM_AGENT_SERVER", ""), "server gRPC address host:port")
	fs.StringVar(&cfg.Token, "token", env("BROKERIUM_AGENT_TOKEN", ""), "join token (first run only)")
	fs.StringVar(&cfg.Transport, "transport", env("BROKERIUM_AGENT_TRANSPORT", agent.TransportMTLS),
		"mtls, or insecure for a server behind a proxy that terminates TLS (messages are signed instead)")
	fs.StringVar(&cfg.TLS, "tls", env("BROKERIUM_AGENT_TLS", agent.TLSSystem),
		"insecure transport: system (verify the proxy with the system CAs and --ca-file), skip-verify or none (plaintext)")
	fs.StringVar(&cfg.CAFile, "ca-file", env("BROKERIUM_AGENT_CA_FILE", ""), "insecure transport: PEM file with extra CAs for --tls system")
	fs.StringVar(&cfg.DataDir, "data-dir", env("BROKERIUM_AGENT_DATA_DIR", "/var/lib/brokerium-agent"), "agent state directory")
	fs.StringVar(&cfg.KafkaDir, "kafka-dir", env("BROKERIUM_AGENT_KAFKA_DIR", "/opt/brokerium/kafka"), "where Kafka releases are installed")
	fs.StringVar(&cfg.KafkaDataDir, "kafka-data-dir", env("BROKERIUM_AGENT_KAFKA_DATA_DIR", "/var/lib/kafka/data"), "Kafka log.dirs")
	fs.StringVar(&cfg.Preinstalled, "preinstalled", env("BROKERIUM_AGENT_PREINSTALLED", ""), "existing Kafka installation to adopt")
	fs.StringVar(&cfg.Process, "process", env("BROKERIUM_AGENT_PROCESS", "child"), "process manager: child or systemd")
	fs.StringVar(&cfg.KafkaConfig, "kafka-config", env("BROKERIUM_AGENT_KAFKA_CONFIG", ""),
		"adopt an existing node: its server.properties, never rewritten (storage is never formatted)")
	fs.StringVar(&cfg.KafkaUser, "kafka-user", env("BROKERIUM_AGENT_KAFKA_USER", "kafka"), "account that runs Kafka under systemd")
	return fs, cfg
}

func run(cfg agent.Config) error {
	if cfg.Server == "" {
		return fmt.Errorf("--server (BROKERIUM_AGENT_SERVER) is required")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := agent.New(cfg, log)
	if err != nil {
		return err
	}
	log.Info("starting brokerium-agent", "version", version.String(), "server", cfg.Server, "transport", cfg.Transport)
	return a.Run(ctx)
}
