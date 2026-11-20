// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/brokerium/brokerium/internal/kafkaconfig"
)

// docsSite serves the Kafka documentation, with the configuration pages of each line in
// <line>/generated/, such as 44/generated/kafka_config.html.
const docsSite = "https://kafka.apache.org"

// docs are the configuration pages of the Kafka documentation of a release line.
type docs struct {
	source kafkaconfig.Docs
	broker map[string]docKey
	topic  map[string]docKey
}

// docKey is what a configuration page says about a key, as the page renders it.
type docKey struct {
	typ, def, importance, updateMode string
}

var errNoPage = errors.New("no such page")

// fetchDocs reads the broker and topic configuration pages of the documentation of a line, or
// of the closest line that has them.
func fetchDocs(ctx context.Context, site string, want line, known []line) (docs, error) {
	for _, l := range closest(want, known) {
		url := fmt.Sprintf("%s/%s/generated/kafka_config.html", site, l.docsPath())
		page, err := fetchPage(ctx, url)
		if errors.Is(err, errNoPage) {
			slog.Info("no broker configuration page", "url", url)
			continue
		}
		if err != nil {
			return docs{}, err
		}
		d := docs{source: kafkaconfig.Docs{Line: l.String(), Broker: url}, broker: parseDocs(page)}
		if !hasUpdateModes(d.broker) {
			return docs{}, fmt.Errorf("%s lists no keys with update modes; has the page changed?", url)
		}
		url = fmt.Sprintf("%s/%s/generated/topic_config.html", site, l.docsPath())
		page, err = fetchPage(ctx, url)
		switch {
		case errors.Is(err, errNoPage):
			slog.Warn("no topic configuration page; topic keys get no importance", "url", url)
		case err != nil:
			return docs{}, err
		default:
			d.source.Topic = url
			d.topic = parseDocs(page)
		}
		return d, nil
	}
	return docs{}, fmt.Errorf("no configuration pages in the Kafka documentation of %s or of a line near it", want)
}

// closest orders the line and the known lines by their distance from it, older lines first.
func closest(want line, known []line) []line {
	all := slices.SortedFunc(slices.Values(append(slices.Clone(known), want)), line.compare)
	all = slices.Compact(all)
	at := slices.Index(all, want)
	out := []line{want}
	for d := 1; len(out) < len(all); d++ {
		if at-d >= 0 {
			out = append(out, all[at-d])
		}
		if at+d < len(all) {
			out = append(out, all[at+d])
		}
	}
	return out
}

var pageClient = &http.Client{Timeout: time.Minute}

// fetchPage reads a page, retrying once; errNoPage means the site answered 404.
func fetchPage(ctx context.Context, url string) (string, error) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var page string
		if page, err = get(ctx, url); err == nil || errors.Is(err, errNoPage) || ctx.Err() != nil {
			return page, err
		}
		slog.Warn("reading the documentation", "url", url, "err", err)
	}
	return "", err
}

func get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := pageClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%s: %w", url, errNoPage)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	return string(body), nil
}

// The configuration pages are rendered by ConfigDef.toHtml of Kafka: per key a heading with an
// anchor named after the key, the documentation and a table of details.
var (
	docHeading = regexp.MustCompile(`<h4>\s*<a id="([^"]+)">\s*</a>`)
	docDetail  = regexp.MustCompile(`(?s)<tr>\s*<th>\s*([^<]+?):\s*</th>\s*<td>(.*?)</td>\s*</tr>`)
)

// parseDocs reads the details of every key of a configuration page.
func parseDocs(page string) map[string]docKey {
	out := map[string]docKey{}
	headings := docHeading.FindAllStringSubmatchIndex(page, -1)
	for i, h := range headings {
		end := len(page)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		var k docKey
		for _, d := range docDetail.FindAllStringSubmatch(page[h[1]:end], -1) {
			value := html.UnescapeString(d[2])
			switch d[1] {
			case "Type":
				k.typ = value
			case "Default":
				k.def = value
			case "Importance":
				k.importance = value
			case "Update Mode":
				k.updateMode = value
			}
		}
		out[html.UnescapeString(page[h[2]:h[3]])] = k
	}
	return out
}

func hasUpdateModes(keys map[string]docKey) bool {
	for _, k := range keys {
		if k.updateMode != "" {
			return true
		}
	}
	return false
}
