// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
)

// vectorKey is the signing key k1 of the test vector; its seed is the bytes 0x01 to 0x20.
func vectorKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

var vectorKeys = map[string]string{"k1": "ebVWLo/mVPlAeLES6KmLp5AfhTrmlb7X4OORC60ElmQ="}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func vectorClaims() Claims {
	return Claims{V: 1, ID: "lic_test0000000000", KID: "k1", Customer: "Example GmbH", Email: "ops@example.com",
		Edition: "enterprise", Features: []string{"message-tools"}, Limits: map[string]int{},
		IssuedAt: at("2026-10-20T12:00:00Z"), NotBefore: at("2026-10-20T00:00:00Z"), ExpiresAt: at("2027-10-20T00:00:00Z"),
		GraceDays: 14}
}

const vectorPayload = `{"v":1,"id":"lic_test0000000000","kid":"k1","customer":"Example GmbH","email":"ops@example.com",` +
	`"edition":"enterprise","features":["message-tools"],"limits":{},"issuedAt":"2026-10-20T12:00:00Z",` +
	`"notBefore":"2026-10-20T00:00:00Z","expiresAt":"2027-10-20T00:00:00Z","graceDays":14}`

const vectorText = "brk-lic-1." +
	"eyJ2IjoxLCJpZCI6ImxpY190ZXN0MDAwMDAwMDAwMCIsImtpZCI6ImsxIiwiY3VzdG9tZXIiOiJFeGFtcGxlIEdtYkgiLCJlbWFpbCI6Im9wc0BleGFt" +
	"cGxlLmNvbSIsImVkaXRpb24iOiJlbnRlcnByaXNlIiwiZmVhdHVyZXMiOlsibWVzc2FnZS10b29scyJdLCJsaW1pdHMiOnt9LCJpc3N1ZWRBdCI6IjIw" +
	"MjYtMTAtMjBUMTI6MDA6MDBaIiwibm90QmVmb3JlIjoiMjAyNi0xMC0yMFQwMDowMDowMFoiLCJleHBpcmVzQXQiOiIyMDI3LTEwLTIwVDAwOjAwOjAw" +
	"WiIsImdyYWNlRGF5cyI6MTR9" +
	".wB18ym6cdASIbXQbdn4gtRgYFI6mo5IRfvTb6U19Qv1pg7719r5CiqoAdbSrjzU-cA_9FM_nJ1ZKsL7gm-FlCA"

// signRaw signs any payload with the key of the test vector.
func signRaw(payload string) string {
	signed := Prefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
	return signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(vectorKey(), []byte(signed)))
}

func mustSign(t *testing.T, change func(*Claims)) string {
	t.Helper()
	c := vectorClaims()
	if change != nil {
		change(&c)
	}
	text, err := Sign(c, vectorKey())
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestVector(t *testing.T) {
	t.Parallel()
	text, err := Sign(vectorClaims(), vectorKey())
	if err != nil {
		t.Fatal(err)
	}
	if text != vectorText {
		t.Fatalf("Sign = %s\nwant   %s", text, vectorText)
	}
	payload, _, _ := strings.Cut(strings.TrimPrefix(text, Prefix), ".")
	if raw, _ := base64.RawURLEncoding.DecodeString(payload); string(raw) != vectorPayload {
		t.Errorf("payload = %s\nwant      %s", raw, vectorPayload)
	}
	c, err := parse(vectorText, vectorKeys)
	if err != nil {
		t.Fatal(err)
	}
	if want := vectorClaims(); !reflect.DeepEqual(*c, want) {
		t.Errorf("parse = %+v\nwant    %+v", *c, want)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	payload, signature, _ := strings.Cut(strings.TrimPrefix(vectorText, Prefix), ".")
	tamperedPayload := Prefix + base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(vectorPayload,
		`"features":["message-tools"]`, `"features":["*"]`, 1))) + "." + signature
	flipped := []byte(signature)
	flipped[0] ^= 'A' ^ 'B'
	tests := []struct {
		name string
		text string
		err  string
	}{
		{"valid", vectorText, ""},
		{"line breaks and spaces", " " + vectorText[:40] + "\n" + vectorText[40:] + "\n", ""},
		{"bad prefix", "brk-lic-2." + payload + "." + signature, "does not start with"},
		{"no signature", Prefix + payload, "not of the form"},
		{"extra segment", vectorText + ".x", "not of the form"},
		{"payload not base64url", Prefix + "a+b/c." + signature, "payload is not base64url"},
		{"signature not base64url", Prefix + payload + ".a+b/c", "signature is not base64url"},
		{"payload not JSON", signRaw("not json"), "not valid JSON"},
		{"tampered payload", tamperedPayload, "signature does not match"},
		{"tampered signature", Prefix + payload + "." + string(flipped), "signature does not match"},
		{"unknown kid", mustSign(t, func(c *Claims) { c.KID = "k9" }), `unknown signing key "k9"`},
		{"unsupported version", signRaw(strings.Replace(vectorPayload, `"v":1`, `"v":2`, 1)), "unsupported key version 2"},
		{"other edition", signRaw(strings.Replace(vectorPayload, `"edition":"enterprise"`, `"edition":"community"`, 1)), `for edition "community"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := parse(tc.text, vectorKeys)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("parse: %v", err)
			case tc.err == "" && c.ID != "lic_test0000000000":
				t.Fatalf("parse = %+v", c)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("parse error = %v, want %q", err, tc.err)
			}
		})
	}
}

func TestState(t *testing.T) {
	t.Parallel()
	c := vectorClaims()
	if got := c.GraceEndsAt(); !got.Equal(at("2027-11-03T00:00:00Z")) {
		t.Errorf("GraceEndsAt = %s", got)
	}
	tests := []struct {
		name string
		now  string
		want string
	}{
		{"not yet valid", "2026-10-19T23:59:59Z", edition.StateInvalid},
		{"valid from notBefore", "2026-10-20T00:00:00Z", edition.StateValid},
		{"valid", "2027-06-01T00:00:00Z", edition.StateValid},
		{"grace from expiresAt", "2027-10-20T00:00:00Z", edition.StateGrace},
		{"grace until its end", "2027-11-02T23:59:59Z", edition.StateGrace},
		{"expired", "2027-11-03T00:00:00Z", edition.StateExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := c.State(at(tc.now)); got != tc.want {
				t.Errorf("State(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
	noGrace := vectorClaims()
	noGrace.GraceDays = 0
	if got := noGrace.State(at("2027-10-20T00:00:00Z")); got != edition.StateExpired {
		t.Errorf("without grace days the key expires at expiresAt, got %s", got)
	}
}

func TestCovers(t *testing.T) {
	t.Parallel()
	listed := vectorClaims()
	all := vectorClaims()
	all.Features = []string{AllFeatures}
	none := vectorClaims()
	none.Features = nil
	tests := []struct {
		name    string
		claims  Claims
		feature string
		want    bool
	}{
		{"listed feature", listed, edition.MessageTools, true},
		{"unlisted feature", listed, "sso", false},
		{"wildcard covers listed features", all, edition.MessageTools, true},
		{"wildcard covers future features", all, "sso", true},
		{"no features", none, edition.MessageTools, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.claims.Covers(tc.feature); got != tc.want {
				t.Errorf("Covers(%s) = %v, want %v", tc.feature, got, tc.want)
			}
		})
	}
}

func TestSignNormalizes(t *testing.T) {
	t.Parallel()
	c := vectorClaims()
	c.Features, c.Limits = nil, nil
	c.IssuedAt = c.IssuedAt.Add(400 * time.Millisecond).In(time.FixedZone("CEST", 2*3600))
	text, err := Sign(c, vectorKey())
	if err != nil {
		t.Fatal(err)
	}
	payload, _, _ := strings.Cut(strings.TrimPrefix(text, Prefix), ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	for _, want := range []string{`"features":[]`, `"limits":{}`, `"issuedAt":"2026-10-20T12:00:00Z"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("payload %s does not contain %s", raw, want)
		}
	}
}

func TestSignRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Claims)
		key    ed25519.PrivateKey
	}{
		{"other version", func(c *Claims) { c.V = 2 }, vectorKey()},
		{"other edition", func(c *Claims) { c.Edition = "community" }, vectorKey()},
		{"no id", func(c *Claims) { c.ID = "" }, vectorKey()},
		{"no kid", func(c *Claims) { c.KID = "" }, vectorKey()},
		{"expires before it starts", func(c *Claims) { c.ExpiresAt = c.NotBefore }, vectorKey()},
		{"negative grace", func(c *Claims) { c.GraceDays = -1 }, vectorKey()},
		{"not a private key", nil, ed25519.PrivateKey("short")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := vectorClaims()
			if tc.change != nil {
				tc.change(&c)
			}
			if text, err := Sign(c, tc.key); err == nil {
				t.Errorf("Sign = %s, want an error", text)
			}
		})
	}
}

func TestBuiltInKeys(t *testing.T) {
	t.Parallel()
	for kid, encoded := range publicKeys {
		if pub, err := base64.StdEncoding.DecodeString(encoded); err != nil || len(pub) != ed25519.PublicKeySize {
			t.Errorf("public key %q is not 32 bytes of standard base64", kid)
		}
	}
	if _, ok := publicKeys["k1"]; !ok {
		t.Error("the build does not trust the signing key k1")
	}
}

// TestLicenseCenterVector parses a key that the license center signed with the vector seed and
// its own field values, so both implementations of the format stay compatible.
func TestLicenseCenterVector(t *testing.T) {
	t.Parallel()
	const text = "brk-lic-1.eyJ2IjoxLCJpZCI6ImxpY18wMTIzNDU2Nzg5YWJjZGVmIiwia2lkIjoiazEiLCJjdXN0b21lciI6IkV4YW1wbGUgR21iSCIsImVtYWlsIjoib3BzQGV4YW1wbGUuY29tIiwiZWRpdGlvbiI6ImVudGVycHJpc2UiLCJmZWF0dXJlcyI6WyJtZXNzYWdlLXRvb2xzIl0sImxpbWl0cyI6e30sImlzc3VlZEF0IjoiMjAyNi0xMC0yMFQxMjowMDowMFoiLCJub3RCZWZvcmUiOiIyMDI2LTEwLTIwVDEyOjAwOjAwWiIsImV4cGlyZXNBdCI6IjIwMjctMTAtMjBUMTI6MDA6MDBaIiwiZ3JhY2VEYXlzIjoxNH0.rclUFDDeHzcjUOm-7FgBC-mHf_jw7a7t0Aq9lkOCSkaw_MLmWM8s1fHNea-AjWe6TJLqtntTouFPsMYk7zWKBQ"
	c, err := parse(text, vectorKeys)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "lic_0123456789abcdef" || c.Customer != "Example GmbH" || c.GraceDays != 14 ||
		!c.ExpiresAt.Equal(time.Date(2027, 10, 20, 12, 0, 0, 0, time.UTC)) || !slices.Equal(c.Features, []string{"message-tools"}) {
		t.Fatalf("parse = %+v", *c)
	}
	if _, err := parse(text, publicKeys); err == nil {
		t.Fatal("a key signed with the vector seed verifies against the production key k1")
	}
}
