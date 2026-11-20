// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command catalog generates the Kafka configuration catalog of internal/kafkaconfig from Kafka
// itself (ADR 0025). For each release it starts a single combined KRaft node in Docker, creates
// a topic and reads DescribeConfigs of the broker and of the topic with synonyms and
// documentation. Update modes and the importance of keys come from the configuration pages of
// the Kafka documentation of the release line.
//
//	go run ./internal/tools/catalog                  # every supported release (make catalog)
//	go run ./internal/tools/catalog -versions 4.4.0  # one release (make catalog VERSIONS=4.4.0)
//
// Releases from 3.7 run the apache/kafka image. Older releases have none: they run the image of
// the lab, brokerium-lab/kafka:<version>, which deploy/lab/kafka/Dockerfile builds from the
// release tarball checked against the SHA-512 sum Apache publishes. The containers are named
// brokerium-catalog-<version> and removed when their release is done.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/brokerium/brokerium/internal/kafkaconfig"
)

// releases are the latest patches of the supported minor releases, the default of -versions.
var releases = []string{"3.5.2", "3.6.2", "3.7.2", "3.8.1", "3.9.2", "4.0.2", "4.1.2", "4.2.2", "4.3.2", "4.4.0"}

func main() {
	versions := flag.String("versions", strings.Join(releases, " "), "Kafka releases to generate, separated by spaces or commas")
	out := flag.String("out", "internal/kafkaconfig/catalog", "directory of the catalog files")
	timeout := flag.Duration("timeout", 10*time.Minute, "time limit for each release, downloads included")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *versions, *out, *timeout); err != nil {
		slog.Error("catalog", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, versions, out string, timeout time.Duration) error {
	list, err := parseReleases(versions)
	if err != nil {
		return err
	}
	var failed []string
	for _, r := range list {
		if ctx.Err() != nil {
			return fmt.Errorf("interrupted before %s", r)
		}
		if err := generate(ctx, r, out, timeout); err != nil {
			slog.Error("release failed", "version", r, "err", err)
			failed = append(failed, r.String())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d releases failed: %s", len(failed), len(list), strings.Join(failed, ", "))
	}
	return nil
}

// generate writes the catalog of one release.
func generate(ctx context.Context, r version, out string, timeout time.Duration) (err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w: the release took longer than -timeout %s", err, timeout)
		}
	}()
	start := time.Now()
	docs, err := fetchDocs(ctx, docsSite, r.line(), supportedLines())
	if err != nil {
		return err
	}
	if docs.source.Line != r.line().String() {
		slog.Warn("no configuration pages in the documentation of the release; using the closest line",
			"version", r, "docs", docs.source.Line)
	}
	n, err := startNode(ctx, r)
	if err != nil {
		return err
	}
	defer n.remove()
	broker, topic, err := n.describe(ctx)
	if err != nil {
		return err
	}
	c, notes, err := buildCatalog(r, docs, broker, topic)
	if err != nil {
		return err
	}
	for _, note := range notes {
		slog.Warn(note, "version", r)
	}
	data, err := kafkaconfig.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	file := filepath.Join(out, c.Line+".json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	slog.Info("wrote the catalog", "file", file, "version", r, "brokerKeys", len(c.Broker), "topicKeys", len(c.Topic),
		"took", time.Since(start).Round(time.Second))
	return nil
}

// parseReleases reads -versions: full releases such as 4.4.0, at most one per minor release.
func parseReleases(s string) ([]version, error) {
	var out []version
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		v, ok := parseVersion(f)
		if !ok {
			return nil, fmt.Errorf("%q is not a Kafka release such as 4.4.0", f)
		}
		if slices.ContainsFunc(out, func(o version) bool { return o.line() == v.line() }) {
			return nil, fmt.Errorf("more than one release of %s", v.line())
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New("no releases to generate")
	}
	return out, nil
}

// supportedLines are the minor releases of the default releases.
func supportedLines() []line {
	out := make([]line, 0, len(releases))
	for _, s := range releases {
		v, _ := parseVersion(s)
		out = append(out, v.line())
	}
	return out
}
