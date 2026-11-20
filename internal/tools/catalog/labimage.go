// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// labDir holds the Dockerfile of the lab for releases without an apache/kafka image and is its
// build context: a release tarball in its dist/ saves the download.
const labDir = "deploy/lab/kafka"

// Mirrors of the SHA-512 sums Apache publishes next to the release tarballs.
var sumMirrors = []string{"https://downloads.apache.org/kafka", "https://archive.apache.org/dist/kafka"}

// labImage returns brokerium-lab/kafka:<version>, the image of the lab for a release without an
// apache/kafka image, and builds it from the release tarball when it is missing.
func labImage(ctx context.Context, v version) (string, error) {
	image := "brokerium-lab/kafka:" + v.String()
	if imagePresent(ctx, image) {
		return image, nil
	}
	dockerfile := filepath.Join(labDir, "Dockerfile")
	if _, err := os.Stat(dockerfile); err != nil {
		return "", fmt.Errorf("building %s needs %s; run the generator from the root of the repository: %w", image, dockerfile, err)
	}
	sum, err := publishedSum(ctx, v)
	if err != nil {
		return "", err
	}
	slog.Info("building the image from the release tarball, which can take minutes to download; a tarball in dist/ saves it",
		"image", image, "dist", filepath.Join(labDir, "dist"))
	_, err = docker(ctx, "build", "--file", dockerfile, "--tag", image,
		"--build-arg", "KAFKA_VERSION="+v.String(), "--build-arg", "KAFKA_SHA512="+sum, labDir)
	return image, err
}

func publishedSum(ctx context.Context, v version) (string, error) {
	file := fmt.Sprintf("kafka_2.13-%s.tgz", v)
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
