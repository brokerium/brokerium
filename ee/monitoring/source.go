// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/store"
)

// Authentication of a source.
const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthBearer = "bearer"
)

// Origins of the source in use.
const (
	OriginNone        = "none"
	OriginEnvironment = "environment"
	OriginDatabase    = "database"
)

// sourceKey is the installation setting that holds the source added through the dashboard.
const sourceKey = "monitoring.prometheus"

// Timeouts of the queries of a source.
const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 5 * time.Minute
)

// ErrManagedExternally is returned for changes of a source that BROKERIUM_PROMETHEUS_URL sets.
var ErrManagedExternally = errors.New("the Prometheus data source is set by BROKERIUM_PROMETHEUS_URL; " +
	"change the environment of the server and restart it")

// ErrBadSource is returned for invalid settings of a source.
var ErrBadSource = errors.New("invalid data source")

// Source is the Prometheus that the monitoring feature queries, with its credentials.
type Source struct {
	URL                string
	Auth               string
	Username           string
	Password           string
	Token              string
	CAPEM              string
	InsecureSkipVerify bool
	Timeout            time.Duration
	Origin             string
	UpdatedAt          time.Time
	UpdatedBy          string
}

// fingerprint identifies the settings and credentials of a source, so that cached clients and
// profiles follow changes.
func (s *Source) fingerprint() string {
	raw, _ := json.Marshal([]any{s.URL, s.Auth, s.Username, s.Password, s.Token, s.CAPEM, s.InsecureSkipVerify, s.Timeout})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// storedSource are the plain settings of a stored source; storedSecrets are sealed.
type storedSource struct {
	URL                string `json:"url"`
	Auth               string `json:"auth"`
	Username           string `json:"username,omitempty"`
	CAPEM              string `json:"caPem,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	TimeoutSeconds     int    `json:"timeoutSeconds,omitempty"`
}

type storedSecrets struct {
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

// SourceRequest changes or tests a source. Password and Token are write-only: when they are
// missing, the stored ones are kept as long as the authentication stays the same.
type SourceRequest struct {
	URL                string  `json:"url"`
	Auth               string  `json:"auth"`
	Username           string  `json:"username"`
	Password           *string `json:"password"`
	Token              *string `json:"token"`
	CAPEM              string  `json:"caPem"`
	InsecureSkipVerify bool    `json:"insecureSkipVerify"`
	TimeoutSeconds     int     `json:"timeoutSeconds"`
}

// SourceView describes the source in use without its credentials.
type SourceView struct {
	Configured         bool       `json:"configured"`
	Origin             string     `json:"origin"`
	URL                string     `json:"url"`
	Auth               string     `json:"auth"`
	Username           string     `json:"username"`
	PasswordSet        bool       `json:"passwordSet"`
	TokenSet           bool       `json:"tokenSet"`
	CAPEM              string     `json:"caPem"`
	InsecureSkipVerify bool       `json:"insecureSkipVerify"`
	TimeoutSeconds     int        `json:"timeoutSeconds"`
	UpdatedAt          *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy          string     `json:"updatedBy,omitempty"`
	// Error tells why the source cannot be used, for example an unreadable credentials file.
	Error string `json:"error,omitempty"`
}

func viewOf(s *Source) SourceView {
	if s == nil {
		return SourceView{Origin: OriginNone, Auth: AuthNone, TimeoutSeconds: int(defaultTimeout.Seconds())}
	}
	v := SourceView{Configured: true, Origin: s.Origin, URL: s.URL, Auth: s.Auth, Username: s.Username,
		PasswordSet: s.Password != "", TokenSet: s.Token != "", CAPEM: s.CAPEM, InsecureSkipVerify: s.InsecureSkipVerify,
		TimeoutSeconds: int(s.Timeout.Seconds()), UpdatedBy: s.UpdatedBy}
	if !s.UpdatedAt.IsZero() {
		at := s.UpdatedAt
		v.UpdatedAt = &at
	}
	return v
}

// checkEnvironment validates the source set by the environment at startup.
func checkEnvironment(p config.Prometheus) error {
	if p.URL == "" {
		if p.Set() {
			return errors.New("BROKERIUM_PROMETHEUS_USERNAME, _PASSWORD_FILE, _TOKEN_FILE and _CA_FILE need BROKERIUM_PROMETHEUS_URL")
		}
		return nil
	}
	if err := checkURL(p.URL); err != nil {
		return fmt.Errorf("BROKERIUM_PROMETHEUS_URL: %w", err)
	}
	switch {
	case p.TokenFile != "" && (p.Username != "" || p.PasswordFile != ""):
		return errors.New("BROKERIUM_PROMETHEUS_TOKEN_FILE (bearer authentication) cannot be combined with " +
			"BROKERIUM_PROMETHEUS_USERNAME and BROKERIUM_PROMETHEUS_PASSWORD_FILE (basic authentication)")
	case p.PasswordFile != "" && p.Username == "":
		return errors.New("BROKERIUM_PROMETHEUS_PASSWORD_FILE needs BROKERIUM_PROMETHEUS_USERNAME")
	}
	return nil
}

// environmentSource reads the source that the environment sets; nil without
// BROKERIUM_PROMETHEUS_URL. The files are read on every call, so replaced Secrets take effect.
func environmentSource(p config.Prometheus) (*Source, error) {
	if p.URL == "" {
		return nil, nil
	}
	s := &Source{URL: p.URL, Auth: AuthNone, Username: p.Username, Timeout: defaultTimeout, Origin: OriginEnvironment}
	read := func(variable, path string) (string, error) {
		raw, err := os.ReadFile(path) //nolint:gosec // the path comes from the environment of the server
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", variable, err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	var err error
	switch {
	case p.TokenFile != "":
		s.Auth = AuthBearer
		if s.Token, err = read("BROKERIUM_PROMETHEUS_TOKEN_FILE", p.TokenFile); err != nil {
			return s, err
		}
	case p.Username != "":
		s.Auth = AuthBasic
		if p.PasswordFile != "" {
			if s.Password, err = read("BROKERIUM_PROMETHEUS_PASSWORD_FILE", p.PasswordFile); err != nil {
				return s, err
			}
		}
	}
	if p.CAFile != "" {
		if s.CAPEM, err = read("BROKERIUM_PROMETHEUS_CA_FILE", p.CAFile); err != nil {
			return s, err
		}
		if err := checkCA(s.CAPEM); err != nil {
			return s, fmt.Errorf("BROKERIUM_PROMETHEUS_CA_FILE: %w", err)
		}
	}
	return s, nil
}

// checkURL accepts the http or https base URL of the Prometheus HTTP API, with an optional path
// prefix but without credentials, a query or a fragment.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return fmt.Errorf("%w: %v", ErrBadSource, err)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("%w: the URL must start with http:// or https://", ErrBadSource)
	case u.Hostname() == "":
		return fmt.Errorf("%w: the URL has no host", ErrBadSource)
	case u.User != nil:
		return fmt.Errorf("%w: put the credentials into the authentication settings, not into the URL", ErrBadSource)
	case u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "?#"):
		return fmt.Errorf("%w: the URL must not have a query or a fragment", ErrBadSource)
	}
	return nil
}

func checkCA(caPEM string) error {
	rest := []byte(caPEM)
	found := false
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("%w: the CA certificate does not parse: %v", ErrBadSource, err)
		}
		found = true
	}
	if !found {
		return fmt.Errorf("%w: the CA certificate is not a PEM certificate", ErrBadSource)
	}
	return nil
}

// Sealer seals and opens the credentials of a stored source; *secrets.Box is one.
type Sealer interface {
	SealJSON(v any) ([]byte, error)
	OpenJSON(data []byte, v any) error
}

// storedSourceOf decodes a stored source.
func storedSourceOf(st *store.Setting, box Sealer) (*Source, error) {
	var plain storedSource
	if err := json.Unmarshal(st.Value, &plain); err != nil {
		return nil, fmt.Errorf("decoding the stored data source: %w", err)
	}
	var secrets storedSecrets
	if err := box.OpenJSON(st.SecretsSealed, &secrets); err != nil {
		return nil, fmt.Errorf("decrypting the credentials of the data source: %w", err)
	}
	s := &Source{URL: plain.URL, Auth: plain.Auth, Username: plain.Username, Password: secrets.Password, Token: secrets.Token,
		CAPEM: plain.CAPEM, InsecureSkipVerify: plain.InsecureSkipVerify, Timeout: defaultTimeout, Origin: OriginDatabase,
		UpdatedAt: st.UpdatedAt, UpdatedBy: st.UpdatedBy}
	if plain.TimeoutSeconds > 0 {
		s.Timeout = time.Duration(plain.TimeoutSeconds) * time.Second
	}
	if s.Auth == "" {
		s.Auth = AuthNone
	}
	return s, nil
}

// apply validates a request and builds the source it describes. Missing credentials come from
// prev, the stored source, when its authentication is the same.
func (req SourceRequest) apply(prev *Source) (*Source, error) {
	s := &Source{URL: strings.TrimSpace(req.URL), Auth: req.Auth, Username: strings.TrimSpace(req.Username),
		CAPEM: strings.TrimSpace(req.CAPEM), InsecureSkipVerify: req.InsecureSkipVerify, Timeout: defaultTimeout,
		Origin: OriginDatabase}
	if s.Auth == "" {
		s.Auth = AuthNone
	}
	if err := checkURL(s.URL); err != nil {
		return nil, err
	}
	switch {
	case req.TimeoutSeconds < 0 || time.Duration(req.TimeoutSeconds)*time.Second > maxTimeout:
		return nil, fmt.Errorf("%w: the timeout must be between 1 and %d seconds", ErrBadSource, int(maxTimeout.Seconds()))
	case req.TimeoutSeconds > 0:
		s.Timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	same := prev != nil && prev.Auth == s.Auth
	switch s.Auth {
	case AuthNone:
		s.Username = ""
	case AuthBasic:
		if s.Username == "" {
			return nil, fmt.Errorf("%w: basic authentication needs a username", ErrBadSource)
		}
		switch {
		case req.Password != nil:
			s.Password = *req.Password
		case same:
			s.Password = prev.Password
		}
	case AuthBearer:
		s.Username = ""
		switch {
		case req.Token != nil:
			s.Token = strings.TrimSpace(*req.Token)
		case same:
			s.Token = prev.Token
		}
		if s.Token == "" {
			return nil, fmt.Errorf("%w: bearer authentication needs a token", ErrBadSource)
		}
	default:
		return nil, fmt.Errorf("%w: authentication must be none, basic or bearer, not %q", ErrBadSource, s.Auth)
	}
	if s.CAPEM != "" {
		if err := checkCA(s.CAPEM); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// setting converts a source into the installation setting that stores it.
func (s *Source) setting(box Sealer, actor string) (*store.Setting, error) {
	plain, err := json.Marshal(storedSource{URL: s.URL, Auth: s.Auth, Username: s.Username, CAPEM: s.CAPEM,
		InsecureSkipVerify: s.InsecureSkipVerify, TimeoutSeconds: int(s.Timeout.Seconds())})
	if err != nil {
		return nil, err
	}
	st := &store.Setting{Key: sourceKey, Value: plain, UpdatedBy: actor}
	if s.Password != "" || s.Token != "" {
		if st.SecretsSealed, err = box.SealJSON(storedSecrets{Password: s.Password, Token: s.Token}); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// loadSource returns the source in use: the environment, else the stored one, else nil.
func (svc *Service) loadSource(ctx context.Context) (*Source, error) {
	if svc.cfg.Prometheus.URL != "" {
		return environmentSource(svc.cfg.Prometheus)
	}
	st, err := svc.store.GetSetting(ctx, sourceKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the data source: %w", err)
	}
	return storedSourceOf(st, svc.box)
}
