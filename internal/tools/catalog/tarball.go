// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Mirrors of the Kafka releases: the CDN serves current releases, the archive all of them.
var (
	tarballMirrors = []string{"https://dlcdn.apache.org/kafka", "https://archive.apache.org/dist/kafka"}
	sumMirrors     = []string{"https://downloads.apache.org/kafka", "https://archive.apache.org/dist/kafka"}
)

var downloadClient = &http.Client{Timeout: 10 * time.Minute}

// tarball returns the release tarball in the cache directory, downloading it first when it is
// missing. Either way the file must match the SHA-512 sum Apache publishes.
func tarball(ctx context.Context, cache string, v version) (string, error) {
	file := fmt.Sprintf("kafka_2.13-%s.tgz", v)
	sum, err := publishedSum(ctx, v, file)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cache, file)
	if got, err := fileSum(path); err == nil && got == sum {
		return path, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	var errs []error
	for _, mirror := range tarballMirrors {
		url := fmt.Sprintf("%s/%s/%s", mirror, v, file)
		slog.Info("downloading the release", "url", url)
		err := download(ctx, url, path, sum)
		if err == nil {
			return path, nil
		}
		slog.Warn("download failed", "url", url, "err", err)
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("downloading %s: %w", file, errors.Join(errs...))
}

// download writes a file through a temporary file that must match the sum.
func download(ctx context.Context, url, path, sum string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha512.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("%s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("%s: SHA-512 %s does not match the published %s", url, got, sum)
	}
	return os.Rename(tmp.Name(), path)
}

func publishedSum(ctx context.Context, v version, file string) (string, error) {
	var errs []error
	for _, mirror := range sumMirrors {
		url := fmt.Sprintf("%s/%s/%s.sha512", mirror, v, file)
		body, err := fetchPage(ctx, url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sum := parseSum(body)
		if sum == "" {
			errs = append(errs, fmt.Errorf("%s: no SHA-512 sum", url))
			continue
		}
		return sum, nil
	}
	return "", fmt.Errorf("the SHA-512 sum of %s: %w", file, errors.Join(errs...))
}

var sha512Hex = regexp.MustCompile(`[0-9a-fA-F]{128}`)

// parseSum reads a .sha512 file of Apache, which splits the sum into groups after the file name
// ("kafka_2.13-3.5.2.tgz: 1A2B 3C4D ...") or has the form of sha512sum. It returns the sum in
// lower case, or "" when there is none.
func parseSum(body string) string {
	if _, rest, ok := strings.Cut(body, ":"); ok {
		body = rest
	}
	return strings.ToLower(sha512Hex.FindString(strings.Join(strings.Fields(body), "")))
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
