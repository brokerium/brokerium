// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"bytes"
	"encoding/json"
	"maps"
	"testing"
)

func TestCursor(t *testing.T) {
	t.Parallel()
	offsets := map[int32]int64{2: 7, 0: 120, 1: 0}
	s := formatCursor(offsets)
	if s != "0:120,1:0,2:7" {
		t.Fatalf("formatCursor = %q", s)
	}
	back, err := parseCursor(s)
	if err != nil || !maps.Equal(back, offsets) {
		t.Fatalf("parseCursor(%q) = %v, %v", s, back, err)
	}
	for _, bad := range []string{"", "0", "0:", ":5", "a:1", "0:b", "-1:5", "0:-5", "0:1,,1:2", "0:1;1:2"} {
		if _, err := parseCursor(bad); err == nil {
			t.Errorf("parseCursor(%q) accepted", bad)
		}
	}
}

func TestDecodeBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, encoding string
		want               []byte
		wantErr            bool
	}{
		{"text", "héllo", "", []byte("héllo"), false},
		{"utf8", "", "utf8", []byte{}, false},
		{"base64 padded", "/wA=", "base64", []byte{0xff, 0x00}, false},
		{"base64 unpadded", "/wA", "base64", []byte{0xff, 0x00}, false},
		{"base64 with line breaks", "/w\nA=\n", "base64", []byte{0xff, 0x00}, false},
		{"base64 empty", "", "base64", []byte{}, false},
		{"invalid base64", "%%%", "base64", nil, true},
		{"one char of base64", "A", "base64", nil, true},
		{"unknown encoding", "x", "hex", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeBytes(tt.in, tt.encoding)
			if (err != nil) != tt.wantErr || !bytes.Equal(got, tt.want) {
				t.Fatalf("decodeBytes(%q, %q) = %v, %v", tt.in, tt.encoding, got, err)
			}
			if err == nil && got == nil {
				t.Fatal("decodeBytes returned nil, which produces a null")
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, in := range [][]byte{[]byte("text"), {0xff, 0xfe, 0x00}, {}} {
		s, enc := encodeBytes(in)
		out, err := decodeBytes(*s, enc)
		if err != nil || !bytes.Equal(in, out) {
			t.Errorf("%v: encoded %q as %s, decoded %v, %v", in, *s, enc, out, err)
		}
	}
}

func TestProduceRecord(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, body string) produceRequest {
		t.Helper()
		var req produceRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		return req
	}
	t.Run("tombstone", func(t *testing.T) {
		t.Parallel()
		rec, err := parse(t, `{"key": "k", "value": null}`).record("t")
		if err != nil || rec.Value != nil || string(rec.Key) != "k" {
			t.Fatalf("record = %+v, %v", rec, err)
		}
	})
	t.Run("empty value is not a tombstone", func(t *testing.T) {
		t.Parallel()
		rec, err := parse(t, `{"value": ""}`).record("t")
		if err != nil || rec.Value == nil || len(rec.Value) != 0 || rec.Key != nil {
			t.Fatalf("record = %+v, %v", rec, err)
		}
	})
	t.Run("binary", func(t *testing.T) {
		t.Parallel()
		rec, err := parse(t, `{"partition": 2, "key": "/wA=", "keyEncoding": "base64", "value": "/w==", "valueEncoding": "base64",
			"headers": [{"key": "trace", "value": "x"}, {"key": "bin", "value": "AP8=", "encoding": "base64"}]}`).record("t")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rec.Key, []byte{0xff, 0x00}) || !bytes.Equal(rec.Value, []byte{0xff}) || rec.Partition != 2 ||
			len(rec.Headers) != 2 || string(rec.Headers[0].Value) != "x" || !bytes.Equal(rec.Headers[1].Value, []byte{0x00, 0xff}) {
			t.Fatalf("record = %+v", rec)
		}
	})
	for name, body := range map[string]string{
		"missing value":        `{"key": "k"}`,
		"number value":         `{"value": 5}`,
		"invalid base64 value": `{"value": "%%%", "valueEncoding": "base64"}`,
		"invalid base64 key":   `{"key": "%%%", "keyEncoding": "base64", "value": ""}`,
		"invalid header":       `{"value": "", "headers": [{"key": "h", "value": "%%%", "encoding": "base64"}]}`,
		"unknown encoding":     `{"value": "", "valueEncoding": "hex"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if rec, err := parse(t, body).record("t"); err == nil {
				t.Fatalf("record = %+v, want an error", rec)
			}
		})
	}
}

func TestWindowDone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		win  window
		want bool
	}{
		{"nothing to read", window{at: 5, stop: 5, first: -1, last: -1}, true},
		{"not started", window{at: 0, stop: 5, first: -1, last: -1}, false},
		{"reached the stop", window{at: 0, stop: 5, first: 0, last: 4, read: 5}, true},
		{"gap at the end", window{at: 0, stop: 5, first: 0, last: 2, read: 3}, false},
		{"passed the stop", window{at: 0, stop: 5, first: 0, last: 2, read: 3, passed: true}, true},
		{"page full", window{at: 0, stop: 100, first: 0, last: 9, read: 10}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.win.done(10); got != tt.want {
				t.Fatalf("done = %v, want %v", got, tt.want)
			}
		})
	}
}
