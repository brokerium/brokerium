// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package licensing signs and verifies Brokerium Enterprise license keys (ADR 0016) and keeps
// track of the key the server uses.
//
// A key is the text brk-lic-1.<payload>.<signature>: the payload is unpadded base64url of the
// JSON claims, the signature is unpadded base64url of the Ed25519 signature over
// "brk-lic-1.<payload>" by the signing key that the claim kid names.
package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
)

// Prefix starts every license key of format version 1.
const Prefix = "brk-lic-1."

// Version is the format version of the claims.
const Version = 1

// AllFeatures in the features of a key covers every feature.
const AllFeatures = "*"

// Claims are the signed contents of a license key. The field order is the order of the JSON
// payload.
type Claims struct {
	V        int      `json:"v"`
	ID       string   `json:"id"`
	KID      string   `json:"kid"`
	Customer string   `json:"customer"`
	Email    string   `json:"email"`
	Edition  string   `json:"edition"`
	Features []string `json:"features"`
	// Limits are reserved; empty means none.
	Limits    map[string]int `json:"limits"`
	IssuedAt  time.Time      `json:"issuedAt"`
	NotBefore time.Time      `json:"notBefore"`
	ExpiresAt time.Time      `json:"expiresAt"`
	GraceDays int            `json:"graceDays"`
}

// GraceEndsAt is the end of the grace period after ExpiresAt.
func (c *Claims) GraceEndsAt() time.Time {
	return c.ExpiresAt.AddDate(0, 0, c.GraceDays)
}

// State returns the state of the license at now: valid from NotBefore until ExpiresAt, then
// grace for GraceDays, then expired. A key used before NotBefore is invalid.
func (c *Claims) State(now time.Time) string {
	switch {
	case now.Before(c.NotBefore):
		return edition.StateInvalid
	case now.Before(c.ExpiresAt):
		return edition.StateValid
	case now.Before(c.GraceEndsAt()):
		return edition.StateGrace
	default:
		return edition.StateExpired
	}
}

// Covers reports whether the claims list the feature or all features.
func (c *Claims) Covers(feature string) bool {
	return slices.Contains(c.Features, feature) || slices.Contains(c.Features, AllFeatures)
}

// Sign creates the license key for the claims. Times are stored in UTC with second precision.
func Sign(c Claims, priv ed25519.PrivateKey) (string, error) {
	switch {
	case len(priv) != ed25519.PrivateKeySize:
		return "", errors.New("the signing key is not an Ed25519 private key")
	case c.V != Version:
		return "", fmt.Errorf("unsupported version %d", c.V)
	case c.Edition != edition.Enterprise:
		return "", fmt.Errorf("edition must be %s", edition.Enterprise)
	case c.ID == "" || c.KID == "":
		return "", errors.New("id and kid are required")
	case !c.ExpiresAt.After(c.NotBefore):
		return "", errors.New("expiresAt must be after notBefore")
	case c.GraceDays < 0:
		return "", errors.New("graceDays must not be negative")
	}
	if c.Features == nil {
		c.Features = []string{}
	}
	if c.Limits == nil {
		c.Limits = map[string]int{}
	}
	c.IssuedAt = c.IssuedAt.UTC().Truncate(time.Second)
	c.NotBefore = c.NotBefore.UTC().Truncate(time.Second)
	c.ExpiresAt = c.ExpiresAt.UTC().Truncate(time.Second)
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signed := Prefix + base64.RawURLEncoding.EncodeToString(raw)
	return signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(signed))), nil
}

// Parse verifies a license key against the public keys built into this binary. Whitespace,
// for example from line breaks in an email, is ignored.
func Parse(text string) (*Claims, error) {
	return parse(text, publicKeys)
}

func normalize(text string) string {
	return strings.Join(strings.Fields(text), "")
}

func parse(text string, keys map[string]string) (*Claims, error) {
	rest, ok := strings.CutPrefix(normalize(text), Prefix)
	if !ok {
		return nil, fmt.Errorf("the key does not start with %q", Prefix)
	}
	payload, sig, ok := strings.Cut(rest, ".")
	if !ok || payload == "" || sig == "" || strings.Contains(sig, ".") {
		return nil, errors.New("the key is not of the form brk-lic-1.<payload>.<signature>")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("the payload is not base64url")
	}
	signature, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, errors.New("the signature is not base64url")
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("the payload is not valid JSON: %w", err)
	}
	if c.V != Version {
		return nil, fmt.Errorf("unsupported key version %d", c.V)
	}
	encoded, ok := keys[c.KID]
	if !ok {
		return nil, fmt.Errorf("unknown signing key %q", c.KID)
	}
	pub, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("the public key %q of this build is malformed", c.KID)
	}
	if !ed25519.Verify(pub, []byte(Prefix+payload), signature) {
		return nil, errors.New("the signature does not match")
	}
	if c.Edition != edition.Enterprise {
		return nil, fmt.Errorf("the key is for edition %q, not %s", c.Edition, edition.Enterprise)
	}
	return &c, nil
}

const testKeyID = "test"

// TrustsTestKey reports whether this build accepts keys signed by the public test key.
func TrustsTestKey() bool {
	_, ok := publicKeys[testKeyID]
	return ok
}
