// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package secrets

import (
	"bytes"
	"testing"
)

func TestNewRejectsWrongKeySize(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 16, 31, 33} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("key of %d bytes accepted", n)
		}
	}
}

func TestSealOpen(t *testing.T) {
	t.Parallel()
	box, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := box.Seal([]byte("secret"))
	if bytes.Equal(a, b) {
		t.Error("two seals of the same plaintext are identical; the nonce is not random")
	}
	got, err := box.Open(a)
	if err != nil || string(got) != "secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}

	tampered := append([]byte(nil), a...)
	tampered[len(tampered)-1] ^= 1
	if _, err := box.Open(tampered); err == nil {
		t.Error("tampered ciphertext opened")
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(a); err == nil {
		t.Error("ciphertext opened with another key")
	}
	if _, err := box.Open([]byte{1, 2}); err == nil {
		t.Error("short ciphertext opened")
	}
}

func TestSealJSON(t *testing.T) {
	t.Parallel()
	box, _ := New(bytes.Repeat([]byte{1}, 32))
	type creds struct{ Password string }
	sealed, err := box.SealJSON(creds{Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var out creds
	if err := box.OpenJSON(sealed, &out); err != nil || out.Password != "p" {
		t.Fatalf("OpenJSON = %+v, %v", out, err)
	}
}
