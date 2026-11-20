// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// A configuration page in the form ConfigDef.toHtml of Kafka renders it.
const testBrokerPage = `<ul class="config-list">
<li>
<h4><a id="advertised.listeners"></a><a id="brokerconfigs_advertised.listeners" href="#brokerconfigs_advertised.listeners">advertised.listeners</a></h4>
<p>Listeners to publish for clients to use:<ul><li>one</li><li>two &amp; more</li></ul></p>
<table><tbody>
<tr><th>Type:</th><td>list</td></tr>
<tr><th>Default:</th><td>null</td></tr>
<tr><th>Valid Values:</th><td></td></tr>
<tr><th>Importance:</th><td>high</td></tr>
<tr><th>Update Mode:</th><td>read-only</td></tr>
</tbody></table>
</li>
<li>
<h4><a id="log.segment.bytes"></a><a id="brokerconfigs_log.segment.bytes" href="#brokerconfigs_log.segment.bytes">log.segment.bytes</a></h4>
<p>The maximum size of a single log file</p>
<table><tbody>
<tr><th>Type:</th><td>int</td></tr>
<tr><th>Default:</th><td>1073741824 (1 gibibyte)</td></tr>
<tr><th>Valid Values:</th><td>[14,...]</td></tr>
<tr><th>Importance:</th><td>high</td></tr>
<tr><th>Update Mode:</th><td>cluster-wide</td></tr>
</tbody></table>
</li>
<li>
<h4><a id="sasl.mechanism.controller.protocol"></a><a id="brokerconfigs_sasl.mechanism.controller.protocol" href="#brokerconfigs_sasl.mechanism.controller.protocol">sasl.mechanism.controller.protocol</a></h4>
<p>SASL mechanism used for communication with controllers. Default is GSSAPI.</p>
<table><tbody>
<tr><th>Type:</th><td>string</td></tr>
<tr><th>Default:</th><td>GSSAPI</td></tr>
<tr><th>Valid Values:</th><td></td></tr>
<tr><th>Importance:</th><td>high</td></tr>
<tr><th>Update Mode:</th><td>read-only</td></tr>
</tbody></table>
</li>
</ul>
`

func TestParseDocs(t *testing.T) {
	t.Parallel()
	got := parseDocs(testBrokerPage)
	want := map[string]docKey{
		"advertised.listeners":               {typ: "list", def: "null", importance: "high", updateMode: "read-only"},
		"log.segment.bytes":                  {typ: "int", def: "1073741824 (1 gibibyte)", importance: "high", updateMode: "cluster-wide"},
		"sasl.mechanism.controller.protocol": {typ: "string", def: "GSSAPI", importance: "high", updateMode: "read-only"},
	}
	if len(got) != len(want) {
		t.Errorf("parseDocs found %d keys, want %d: %v", len(got), len(want), got)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: %+v, want %+v", name, got[name], w)
		}
	}
	if keys := parseDocs("<html>Not found</html>"); len(keys) != 0 || hasUpdateModes(keys) {
		t.Errorf("parseDocs of another page: %v", keys)
	}
}

func TestClosest(t *testing.T) {
	t.Parallel()
	known := []line{{3, 5}, {3, 6}, {3, 7}, {3, 8}, {3, 9}, {4, 0}, {4, 1}}
	tests := []struct {
		want line
		out  []line
	}{
		{line{3, 9}, []line{{3, 9}, {3, 8}, {4, 0}, {3, 7}, {4, 1}, {3, 6}, {3, 5}}},
		{line{4, 2}, []line{{4, 2}, {4, 1}, {4, 0}, {3, 9}, {3, 8}, {3, 7}, {3, 6}, {3, 5}}},
		{line{3, 4}, []line{{3, 4}, {3, 5}, {3, 6}, {3, 7}, {3, 8}, {3, 9}, {4, 0}, {4, 1}}},
	}
	for _, tt := range tests {
		if got := closest(tt.want, known); !slices.Equal(got, tt.out) {
			t.Errorf("closest(%s) = %v, want %v", tt.want, got, tt.out)
		}
	}
}

func TestFetchDocsFallsBack(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/43/generated/kafka_config.html":
			_, _ = w.Write([]byte(testBrokerPage))
		case "/43/generated/topic_config.html":
			_, _ = w.Write([]byte(`<h4><a id="retention.ms"></a></h4><table><tbody>` +
				`<tr><th>Type:</th><td>long</td></tr><tr><th>Importance:</th><td>medium</td></tr></tbody></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d, err := fetchDocs(context.Background(), srv.URL, line{4, 4}, []line{{4, 2}, {4, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if d.source.Line != "4.3" || d.source.Broker != srv.URL+"/43/generated/kafka_config.html" ||
		d.source.Topic != srv.URL+"/43/generated/topic_config.html" {
		t.Errorf("docs source %+v, want the pages of 4.3", d.source)
	}
	if d.broker["log.segment.bytes"].updateMode != "cluster-wide" || d.topic["retention.ms"].importance != "medium" {
		t.Errorf("docs %+v", d)
	}
	if _, err := fetchDocs(context.Background(), srv.URL, line{4, 4}, []line{{4, 2}}); err == nil {
		t.Error("fetchDocs found pages where there are none")
	}
}
