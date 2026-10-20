// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise && licensetest

package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestTestKey(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/test-signing.key")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("testdata/test-signing.key is not a base64 Ed25519 seed: %v", err)
	}
	c := vectorClaims()
	c.KID = testKeyID
	text, err := Sign(c, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(text); err != nil {
		t.Fatalf("the test key does not verify a key it signed: %v", err)
	}
	if !TrustsTestKey() {
		t.Error("TrustsTestKey = false in a licensetest build")
	}
}
