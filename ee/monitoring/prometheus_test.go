// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientQueries(t *testing.T) {
	t.Parallel()
	prom := newFakeProm(t, standardAnswer)
	c, err := NewClient(&Source{URL: prom.URL + "/prometheus/", Auth: AuthBasic, Username: "brokerium", Password: "s3cret",
		Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	at := time.UnixMilli(1_763_640_000_500)
	res, err := c.Query(ctx, "vector(1)", at)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 1 || len(res.Series[0].Points) != 1 || res.Series[0].Points[0] != (Point{T: 1763640000, V: 1}) {
		t.Errorf("vector(1) = %+v", res.Series)
	}
	start := time.Unix(1_763_636_400, 0)
	res, err = c.QueryRange(ctx, `sum by (instance) (process_open_fds{job="kafka"})`, start, start.Add(time.Hour), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 3 || len(res.Series[0].Points) != 1 || res.Series[2].Points[1].V != 1.5 {
		t.Errorf("the range query parsed %+v; NaN must be dropped", res.Series)
	}
	version, err := c.BuildInfo(ctx)
	if err != nil || version != "3.7.3" {
		t.Errorf("BuildInfo = %q, %v", version, err)
	}
	reqs := prom.queries()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v", reqs)
	}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("brokerium:s3cret"))
	for _, r := range reqs {
		if r.Auth != basic {
			t.Errorf("%s: Authorization = %q", r.Endpoint, r.Auth)
		}
	}
	if r := reqs[0]; r.Method != http.MethodPost || r.Endpoint != "query" || r.Form.Get("time") != "1763640000.5" {
		t.Errorf("instant query = %+v", r)
	}
	if f := reqs[1].Form; reqs[1].Endpoint != "query_range" || f.Get("start") != "1763636400" || f.Get("end") != "1763640000" ||
		f.Get("step") != "15" || f.Get("query") != `sum by (instance) (process_open_fds{job="kafka"})` {
		t.Errorf("range query = %+v", reqs[1])
	}
	if reqs[2].Method != http.MethodGet || reqs[2].Endpoint != "status/buildinfo" {
		t.Errorf("build info = %+v", reqs[2])
	}
}

func TestClientResultTypes(t *testing.T) {
	t.Parallel()
	answers := map[string]string{
		"scalar": `{"status":"success","data":{"resultType":"scalar","result":[1763640000,"42"]}}`,
		"warn": `{"status":"success","warnings":["metric might not be a counter"],"data":{"resultType":"vector","result":[
			{"metric":{"job":"kafka"},"value":[1763640000,"+Inf"]}]}}`,
		"string": `{"status":"success","data":{"resultType":"string","result":[1763640000,"x"]}}`,
	}
	prom := newFakeProm(t, func(_ string, form url.Values) (int, string) { return http.StatusOK, answers[form.Get("query")] })
	c, err := NewClient(&Source{URL: prom.URL, Auth: AuthBearer, Token: "tok", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if res, err := c.Query(ctx, "scalar", time.Now()); err != nil || len(res.Series) != 1 || res.Series[0].Points[0].V != 42 {
		t.Errorf("scalar = %+v, %v", res, err)
	}
	res, err := c.Query(ctx, "warn", time.Now())
	if err != nil || len(res.Warnings) != 1 || len(res.Series) != 1 || len(res.Series[0].Points) != 0 || res.Series[0].Labels["job"] != "kafka" {
		t.Errorf("a vector with an infinite value and a warning = %+v, %v", res, err)
	}
	if _, err := c.Query(ctx, "string", time.Now()); !errors.Is(err, ErrResponse) {
		t.Errorf("a string result = %v, want ErrResponse", err)
	}
	if reqs := prom.queries(); reqs[0].Auth != "Bearer tok" {
		t.Errorf("Authorization = %q", reqs[0].Auth)
	}
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   error
		text   string
	}{
		{"credentials", http.StatusUnauthorized, `Unauthorized`, ErrRefused, "HTTP 401"},
		{"forbidden", http.StatusForbidden, `{}`, ErrRefused, "HTTP 403"},
		{"bad query", http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"parse error at char 3"}`, ErrQuery, "parse error"},
		{"execution", http.StatusUnprocessableEntity, `{"status":"error","errorType":"execution","error":"too many samples"}`, ErrQuery, "too many samples"},
		{"query timeout", http.StatusServiceUnavailable, `{"status":"error","errorType":"timeout","error":"query timed out"}`, ErrTimeout, "timed out"},
		{"other error", http.StatusServiceUnavailable, `{"status":"error","errorType":"unavailable","error":"overloaded"}`, ErrResponse, "overloaded"},
		{"proxy page", http.StatusBadGateway, "<html>\n<body>Bad Gateway</body></html>", ErrResponse, "HTTP 502 <html> <body>Bad Gateway"},
		{"not the API", http.StatusOK, `{"hello":"world"}`, ErrResponse, "HTTP 200"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prom := newFakeProm(t, func(string, url.Values) (int, string) { return tt.status, tt.body })
			c, err := NewClient(&Source{URL: prom.URL, Auth: AuthNone, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Query(context.Background(), "up", time.Now())
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.text) {
				t.Errorf("error = %v, want %v containing %q", err, tt.want, tt.text)
			}
		})
	}
}

func TestClientTransportErrors(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + listener.Addr().String()
	_ = listener.Close()
	c, err := NewClient(&Source{URL: closed, Auth: AuthNone, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Query(context.Background(), "up", time.Now()); !errors.Is(err, ErrUnreachable) {
		t.Errorf("a closed port = %v, want ErrUnreachable", err)
	}

	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(block) })
	c, err = NewClient(&Source{URL: slow.URL, Auth: AuthNone, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Query(context.Background(), "up", time.Now()); !errors.Is(err, ErrTimeout) {
		t.Errorf("a server that does not answer = %v, want ErrTimeout", err)
	}
}

func TestClientTLS(t *testing.T) {
	t.Parallel()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	for _, tt := range []struct {
		name   string
		source Source
		ok     bool
	}{
		{"unknown CA", Source{URL: srv.URL}, false},
		{"CA certificate", Source{URL: srv.URL, CAPEM: ca}, true},
		{"verification off", Source{URL: srv.URL, InsecureSkipVerify: true}, true},
	} {
		tt.source.Timeout = 5 * time.Second
		c, err := NewClient(&tt.source)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Query(context.Background(), "up", time.Now())
		if (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	if _, err := NewClient(&Source{URL: srv.URL, CAPEM: "not a certificate"}); err == nil {
		t.Error("NewClient accepted a CA that is not PEM")
	}
}
