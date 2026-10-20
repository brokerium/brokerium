// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/api"
)

// ExportRequest writes the matching records of a topic to a file.
type ExportRequest struct {
	Selection
	Filter
	// Format is ndjson (the default), json or csv.
	Format string `json:"format,omitempty"`
	// Limit bounds the exported records: a million by default and at most.
	Limit int64 `json:"limit,omitempty"`
	// TimeoutSeconds bounds the time spent: 600 by default, at most 3600.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

const (
	maxExportRecords     = 1_000_000
	defaultExportTimeout = 10 * time.Minute
	maxExportTimeout     = time.Hour
)

// exportRecord is one exported record with complete values.
type exportRecord struct {
	Partition     int32               `json:"partition"`
	Offset        int64               `json:"offset"`
	Timestamp     time.Time           `json:"timestamp"`
	Key           *string             `json:"key"`
	KeyEncoding   string              `json:"keyEncoding"`
	Value         *string             `json:"value"`
	ValueEncoding string              `json:"valueEncoding"`
	Headers       []api.MessageHeader `json:"headers"`
}

// encode returns text as is and other bytes as base64; nil stays nil.
func encode(b []byte) (*string, string) {
	if b == nil {
		return nil, "utf8"
	}
	if utf8.Valid(b) {
		s := string(b)
		return &s, "utf8"
	}
	s := base64.StdEncoding.EncodeToString(b)
	return &s, "base64"
}

func newExportRecord(rec *kgo.Record) exportRecord {
	out := exportRecord{Partition: rec.Partition, Offset: rec.Offset, Timestamp: rec.Timestamp, Headers: []api.MessageHeader{}}
	out.Key, out.KeyEncoding = encode(rec.Key)
	out.Value, out.ValueEncoding = encode(rec.Value)
	for _, h := range rec.Headers {
		header := api.MessageHeader{Key: h.Key}
		if v, enc := encode(h.Value); v != nil {
			header.Value = *v
			if enc == "base64" {
				header.Encoding = enc
			}
		}
		out.Headers = append(out.Headers, header)
	}
	return out
}

// exportWriter writes records in one format.
type exportWriter interface {
	write(rec *kgo.Record) error
	close() error
}

// exportFormat describes an export format.
type exportFormat struct {
	contentType string
	extension   string
	open        func(w io.Writer) exportWriter
}

var exportFormats = map[string]exportFormat{
	"ndjson": {"application/x-ndjson", "ndjson", func(w io.Writer) exportWriter { return &ndjsonWriter{enc: json.NewEncoder(w)} }},
	"json":   {"application/json", "json", func(w io.Writer) exportWriter { return &jsonWriter{w: w} }},
	"csv":    {"text/csv; charset=utf-8", "csv", func(w io.Writer) exportWriter { return &csvWriter{w: csv.NewWriter(w)} }},
}

func lookupFormat(name string) (exportFormat, error) {
	if name == "" {
		name = "ndjson"
	}
	f, ok := exportFormats[name]
	if !ok {
		return exportFormat{}, fmt.Errorf("%w: format must be ndjson, json or csv", ErrBadFilter)
	}
	return f, nil
}

type ndjsonWriter struct {
	enc *json.Encoder
}

func (n *ndjsonWriter) write(rec *kgo.Record) error { return n.enc.Encode(newExportRecord(rec)) }
func (n *ndjsonWriter) close() error                { return nil }

type jsonWriter struct {
	w     io.Writer
	count int
}

func (j *jsonWriter) write(rec *kgo.Record) error {
	sep := ",\n"
	if j.count == 0 {
		sep = "[\n"
	}
	j.count++
	b, err := json.Marshal(newExportRecord(rec))
	if err != nil {
		return err
	}
	_, err = io.WriteString(j.w, sep+string(b))
	return err
}

func (j *jsonWriter) close() error {
	end := "\n]\n"
	if j.count == 0 {
		end = "[]\n"
	}
	_, err := io.WriteString(j.w, end)
	return err
}

type csvWriter struct {
	w      *csv.Writer
	header bool
}

func (c *csvWriter) writeHeader() error {
	if c.header {
		return nil
	}
	c.header = true
	return c.w.Write([]string{"partition", "offset", "timestamp", "key", "key_encoding", "value", "value_encoding", "headers"})
}

func (c *csvWriter) write(rec *kgo.Record) error {
	if err := c.writeHeader(); err != nil {
		return err
	}
	r := newExportRecord(rec)
	headers, err := json.Marshal(r.Headers)
	if err != nil {
		return err
	}
	// The encoding column tells a null apart from an empty text.
	text := func(s *string, enc string) (string, string) {
		if s == nil {
			return "", "null"
		}
		return *s, enc
	}
	key, keyEnc := text(r.Key, r.KeyEncoding)
	value, valueEnc := text(r.Value, r.ValueEncoding)
	return c.w.Write([]string{strconv.Itoa(int(r.Partition)), strconv.FormatInt(r.Offset, 10), r.Timestamp.UTC().Format(time.RFC3339Nano),
		key, keyEnc, value, valueEnc, string(headers)})
}

func (c *csvWriter) close() error {
	if err := c.writeHeader(); err != nil {
		return err
	}
	c.w.Flush()
	return c.w.Error()
}
