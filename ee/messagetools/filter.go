// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Match selects bytes that contain a text or match a regular expression (RE2 syntax).
type Match struct {
	Contains string `json:"contains,omitempty"`
	Regex    string `json:"regex,omitempty"`
}

// HeaderMatch selects records with a header of this key whose value matches.
type HeaderMatch struct {
	Key   string `json:"key"`
	Value *Match `json:"value,omitempty"`
}

// Filter selects records; empty fields match every record.
type Filter struct {
	Key        *Match       `json:"key,omitempty"`
	Value      *Match       `json:"value,omitempty"`
	Header     *HeaderMatch `json:"header,omitempty"`
	IgnoreCase bool         `json:"ignoreCase,omitempty"`
}

// ErrBadFilter is returned for filters that cannot be compiled.
var ErrBadFilter = errors.New("invalid filter")

// maxPattern bounds the length of texts and expressions in a filter.
const maxPattern = 1024

type matcher func([]byte) bool

func (m *Match) compile(what string, ignoreCase bool) (matcher, error) {
	switch {
	case m == nil || m.Contains == "" && m.Regex == "":
		return nil, nil
	case m.Contains != "" && m.Regex != "":
		return nil, fmt.Errorf("%w: %s: set either contains or regex", ErrBadFilter, what)
	case len(m.Contains) > maxPattern || len(m.Regex) > maxPattern:
		return nil, fmt.Errorf("%w: %s: longer than %d bytes", ErrBadFilter, what, maxPattern)
	}
	expr := m.Regex
	if expr == "" {
		if !ignoreCase {
			needle := []byte(m.Contains)
			return func(b []byte) bool { return bytes.Contains(b, needle) }, nil
		}
		expr = regexp.QuoteMeta(m.Contains)
	}
	if ignoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrBadFilter, what, err)
	}
	return re.Match, nil
}

// compile returns a function that reports whether a record matches the filter and lies in the
// time range [from, to) in Unix milliseconds; nil bounds are open.
func (f Filter) compile(from, to *int64) (func(*kgo.Record) bool, error) {
	key, err := f.Key.compile("key", f.IgnoreCase)
	if err != nil {
		return nil, err
	}
	value, err := f.Value.compile("value", f.IgnoreCase)
	if err != nil {
		return nil, err
	}
	var header matcher
	var headerKey string
	if f.Header != nil {
		if f.Header.Key == "" {
			return nil, fmt.Errorf("%w: header: the key is required", ErrBadFilter)
		}
		headerKey = f.Header.Key
		if header, err = f.Header.Value.compile("header value", f.IgnoreCase); err != nil {
			return nil, err
		}
	}
	return func(rec *kgo.Record) bool {
		ms := rec.Timestamp.UnixMilli()
		switch {
		case from != nil && ms < *from, to != nil && ms >= *to:
			return false
		case key != nil && !key(rec.Key), value != nil && !value(rec.Value):
			return false
		case headerKey == "":
			return true
		}
		for _, h := range rec.Headers {
			if h.Key == headerKey && (header == nil || header(h.Value)) {
				return true
			}
		}
		return false
	}, nil
}
