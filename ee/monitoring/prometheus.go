// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Errors of the Prometheus client; the API maps each to its own status.
var (
	ErrUnreachable = errors.New("cannot reach Prometheus")
	ErrTimeout     = errors.New("no answer from Prometheus in time")
	ErrRefused     = errors.New("the credentials were refused by Prometheus")
	ErrQuery       = errors.New("the query was rejected by Prometheus")
	ErrResponse    = errors.New("unexpected response from Prometheus")
)

// maxResponseBytes bounds the response of one query.
const maxResponseBytes = 32 << 20

// Point is a value at a time in Unix seconds.
type Point struct {
	T float64
	V float64
}

// Series is a time series of a query result; instant queries have one point per series.
type Series struct {
	Labels map[string]string
	Points []Point
}

// Result is the result of a query.
type Result struct {
	Series   []Series
	Warnings []string
}

// Client queries the HTTP API of Prometheus or of a server with the same API, such as Thanos,
// Mimir or VictoriaMetrics.
type Client struct {
	base *url.URL
	http *http.Client
	auth func(*http.Request)
}

// NewClient builds a client for a source.
func NewClient(s *Source) (*Client, error) {
	base, err := url.Parse(s.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing the URL: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12,
		InsecureSkipVerify: s.InsecureSkipVerify} //nolint:gosec // an administrator turned verification off for this source
	if s.CAPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(s.CAPEM)) {
			return nil, errors.New("the CA certificate is not valid PEM")
		}
		tlsConfig.RootCAs = pool
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	transport.MaxIdleConnsPerHost = 8
	c := &Client{base: base, http: &http.Client{Transport: transport, Timeout: s.Timeout}}
	switch s.Auth {
	case AuthBasic:
		username, password := s.Username, s.Password
		c.auth = func(r *http.Request) { r.SetBasicAuth(username, password) }
	case AuthBearer:
		token := s.Token
		c.auth = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	}
	return c, nil
}

// Close releases idle connections.
func (c *Client) Close() {
	c.http.CloseIdleConnections()
}

// Query runs an instant query.
func (c *Client) Query(ctx context.Context, expr string, at time.Time) (*Result, error) {
	return c.query(ctx, "query", url.Values{"query": {expr}, "time": {formatTime(at)}})
}

// QueryRange runs a range query.
func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (*Result, error) {
	return c.query(ctx, "query_range", url.Values{"query": {expr}, "start": {formatTime(start)}, "end": {formatTime(end)},
		"step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}})
}

// BuildInfo returns the version of the server. Servers without the endpoint return an error.
func (c *Client) BuildInfo(ctx context.Context) (string, error) {
	env, err := c.do(ctx, http.MethodGet, "status/buildinfo", nil)
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		return "", fmt.Errorf("%w: %v", ErrResponse, err)
	}
	return info.Version, nil
}

func (c *Client) query(ctx context.Context, endpoint string, form url.Values) (*Result, error) {
	env, err := c.do(ctx, http.MethodPost, endpoint, form)
	if err != nil {
		return nil, err
	}
	var data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResponse, err)
	}
	res := &Result{Series: []Series{}, Warnings: env.Warnings}
	switch data.ResultType {
	case "matrix":
		var matrix []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		}
		if err := json.Unmarshal(data.Result, &matrix); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrResponse, err)
		}
		for _, m := range matrix {
			s := Series{Labels: m.Metric, Points: make([]Point, 0, len(m.Values))}
			for _, v := range m.Values {
				if p, ok := parsePoint(v); ok {
					s.Points = append(s.Points, p)
				}
			}
			res.Series = append(res.Series, s)
		}
	case "vector":
		var vector []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		}
		if err := json.Unmarshal(data.Result, &vector); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrResponse, err)
		}
		for _, v := range vector {
			s := Series{Labels: v.Metric}
			if p, ok := parsePoint(v.Value); ok {
				s.Points = []Point{p}
			}
			res.Series = append(res.Series, s)
		}
	case "scalar":
		var scalar [2]any
		if err := json.Unmarshal(data.Result, &scalar); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrResponse, err)
		}
		s := Series{Labels: map[string]string{}}
		if p, ok := parsePoint(scalar); ok {
			s.Points = []Point{p}
		}
		res.Series = append(res.Series, s)
	default:
		return nil, fmt.Errorf("%w: result type %q", ErrResponse, data.ResultType)
	}
	for i := range res.Series {
		if res.Series[i].Labels == nil {
			res.Series[i].Labels = map[string]string{}
		}
	}
	return res, nil
}

// envelope is the response of every endpoint of the Prometheus HTTP API.
type envelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
}

// do calls an endpoint below /api/v1 and returns a successful response.
func (c *Client) do(ctx context.Context, method, endpoint string, form url.Values) (*envelope, error) {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/v1/" + endpoint
	u.RawPath = ""
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body) //nolint:gosec // the Prometheus an administrator set
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.auth != nil {
		c.auth(req)
	}
	resp, err := c.http.Do(req) //nolint:gosec // the Prometheus an administrator set
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportError(err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("%w: the response is larger than %d MiB", ErrResponse, maxResponseBytes>>20)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrRefused, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Status == "" {
		return nil, fmt.Errorf("%w: HTTP %d %s", ErrResponse, resp.StatusCode, snippet(raw))
	}
	if env.Status != "success" {
		switch env.ErrorType {
		case "timeout", "canceled":
			return nil, fmt.Errorf("%w: %s", ErrTimeout, env.Error)
		case "bad_data", "execution":
			return nil, fmt.Errorf("%w: %s", ErrQuery, env.Error)
		}
		return nil, fmt.Errorf("%w: HTTP %d, %s: %s", ErrResponse, resp.StatusCode, env.ErrorType, env.Error)
	}
	return &env, nil
}

// transportError classifies an error of the HTTP client.
func transportError(err error) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return fmt.Errorf("%w: %v", ErrUnreachable, err)
}

func parsePoint(v [2]any) (Point, bool) {
	t, ok := v[0].(float64)
	if !ok {
		return Point{}, false
	}
	s, ok := v[1].(string)
	if !ok {
		return Point{}, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return Point{}, false
	}
	return Point{T: t, V: f}, true
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', -1, 64)
}

// snippet returns the start of a response body for an error message.
func snippet(raw []byte) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
