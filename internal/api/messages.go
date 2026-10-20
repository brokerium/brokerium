// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/kafka"
)

// MessageHeader is one record header.
type MessageHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Encoding is "base64" for values that are not UTF-8 and empty for text.
	Encoding string `json:"encoding,omitempty"`
}

// Message is a record shown in the dashboard.
type Message struct {
	Partition     int32           `json:"partition"`
	Offset        int64           `json:"offset"`
	Timestamp     time.Time       `json:"timestamp"`
	Key           *string         `json:"key"`
	Value         *string         `json:"value"`
	Headers       []MessageHeader `json:"headers"`
	KeyEncoding   string          `json:"keyEncoding"`
	ValueEncoding string          `json:"valueEncoding"`
}

const maxValueBytes = 64 << 10

func encodeBytes(b []byte) (*string, string) {
	if b == nil {
		return nil, "utf8"
	}
	if len(b) > maxValueBytes {
		b = b[:maxValueBytes]
	}
	if utf8.Valid(b) {
		s := string(b)
		return &s, "utf8"
	}
	s := base64.StdEncoding.EncodeToString(b)
	return &s, "base64"
}

// decodeBytes reverses encodeBytes for produced records. Base64 may come with or without
// padding and with line breaks.
func decodeBytes(s, encoding string) ([]byte, error) {
	switch encoding {
	case "", "utf8":
		return []byte(s), nil
	case "base64":
		clean := strings.Join(strings.Fields(s), "")
		if b, err := base64.StdEncoding.DecodeString(clean); err == nil {
			return b, nil
		}
		b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
		if err != nil {
			return nil, errors.New("invalid base64")
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown encoding %q", encoding)
	}
}

func toMessage(rec *kgo.Record) Message {
	m := Message{Partition: rec.Partition, Offset: rec.Offset, Timestamp: rec.Timestamp, Headers: []MessageHeader{}}
	m.Key, m.KeyEncoding = encodeBytes(rec.Key)
	m.Value, m.ValueEncoding = encodeBytes(rec.Value)
	for _, h := range rec.Headers {
		header := MessageHeader{Key: h.Key}
		if val, enc := encodeBytes(h.Value); val != nil {
			header.Value = *val
			if enc == "base64" {
				header.Encoding = enc
			}
		}
		m.Headers = append(m.Headers, header)
	}
	return m
}

// formatCursor lists an offset for each partition: "0:120,1:98".
func formatCursor(offsets map[int32]int64) string {
	parts := make([]int32, 0, len(offsets))
	for p := range offsets {
		parts = append(parts, p)
	}
	slices.Sort(parts)
	pairs := make([]string, len(parts))
	for i, p := range parts {
		pairs[i] = fmt.Sprintf("%d:%d", p, offsets[p])
	}
	return strings.Join(pairs, ",")
}

func parseCursor(s string) (map[int32]int64, error) {
	offsets := map[int32]int64{}
	for pair := range strings.SplitSeq(s, ",") {
		p, o, ok := strings.Cut(pair, ":")
		partition, perr := strconv.ParseInt(p, 10, 32)
		offset, oerr := strconv.ParseInt(o, 10, 64)
		if !ok || perr != nil || oerr != nil || partition < 0 || offset < 0 {
			return nil, fmt.Errorf("invalid cursor %q", s)
		}
		offsets[int32(partition)] = offset
	}
	return offsets, nil
}

// window is the range of one partition that a browse request reads.
type window struct {
	start, end  int64 // log start and end offsets
	at, stop    int64 // the first offset to read and the offset to stop before
	first, last int64 // offsets of the first and the last record read, -1 before any
	read        int
	passed      bool // a record at or after stop arrived
}

func (w *window) done(perPartition int) bool {
	return w.read >= perPartition || w.passed || max(w.at, w.last+1) >= w.stop
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	_, v, ok := s.capable(w, r, "browseMessages")
	if !ok {
		return
	}
	q := r.URL.Query()
	topic := r.PathValue("topic")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	partition := int32(-1)
	if p := q.Get("partition"); p != "" {
		n, err := strconv.ParseInt(p, 10, 32)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid partition")
			return
		}
		partition = int32(n)
	}
	from := q.Get("from")
	if from == "" {
		from = "latest"
	}
	var cursor map[int32]int64
	if from == "after" || from == "before" {
		c, err := parseCursor(q.Get("cursor"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		cursor = c
	}
	var parts []int32
	for _, t := range v.Snapshot.Topics {
		if t.Name != topic {
			continue
		}
		for _, p := range t.Partitions {
			_, listed := cursor[p.ID]
			if cursor != nil && listed || cursor == nil && (partition < 0 || p.ID == partition) {
				parts = append(parts, p.ID)
			}
		}
	}
	if len(parts) == 0 {
		WriteError(w, http.StatusNotFound, "not_found", "topic or partition not found")
		return
	}
	cl, _, err := s.Clusters.Client(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	ctx, cancel := timeout(r)
	defer cancel()
	starts, _ := cl.Adm.ListStartOffsets(ctx, topic)
	ends, _ := cl.Adm.ListEndOffsets(ctx, topic)
	var afterMilli kadm.ListedOffsets
	if from == "timestamp" {
		ms, _ := strconv.ParseInt(q.Get("timestamp"), 10, 64)
		afterMilli, _ = cl.Adm.ListOffsetsAfterMilli(ctx, ms, topic)
	}
	perPartition := max(1, limit/len(parts))
	windows := map[int32]*window{}
	offsets := map[int32]kgo.Offset{}
	for _, p := range parts {
		win := &window{first: -1, last: -1}
		if o, ok := starts.Lookup(topic, p); ok {
			win.start = o.Offset
		}
		if o, ok := ends.Lookup(topic, p); ok {
			win.end = o.Offset
		}
		win.stop = win.end
		switch from {
		case "earliest":
			win.at = win.start
		case "offset":
			n, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
			win.at = max(win.start, n)
		case "timestamp":
			win.at = win.end
			if o, ok := afterMilli.Lookup(topic, p); ok {
				win.at = max(win.start, o.Offset)
			}
		case "after":
			win.at = max(win.start, cursor[p])
		case "before":
			win.stop = min(win.end, max(win.start, cursor[p]))
			win.at = max(win.start, win.stop-int64(perPartition))
		default:
			win.at = max(win.start, win.end-int64(perPartition))
		}
		windows[p] = win
		if win.at < win.stop {
			offsets[p] = kgo.NewOffset().At(win.at)
		}
	}
	msgs := []Message{}
	if len(offsets) > 0 {
		profile, err := s.profileOf(r)
		if err != nil {
			WriteStoreError(w, err)
			return
		}
		consumer, err := kafka.NewClient(profile, kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: offsets}),
			kgo.FetchMaxWait(500*time.Millisecond))
		if err != nil {
			kafkaErr(w, err)
			return
		}
		defer consumer.Close()
		pending := func() bool {
			for p := range offsets {
				if !windows[p].done(perPartition) {
					return true
				}
			}
			return false
		}
		deadline := time.Now().Add(5 * time.Second)
		for pending() && len(msgs) < limit && time.Now().Before(deadline) {
			wait := time.Until(deadline)
			if len(msgs) > 0 {
				// Compaction and transaction markers leave offsets that no record fills.
				wait = min(wait, time.Second)
			}
			pctx, pcancel := context.WithTimeout(ctx, wait)
			fetches := consumer.KGO.PollFetches(pctx)
			idle := pctx.Err() != nil
			pcancel()
			got := 0
			fetches.EachRecord(func(rec *kgo.Record) {
				win := windows[rec.Partition]
				switch {
				case win == nil || win.read >= perPartition || len(msgs) >= limit:
				case rec.Offset >= win.stop:
					win.passed = true
				default:
					if win.first < 0 {
						win.first = rec.Offset
					}
					win.last = rec.Offset
					win.read++
					got++
					msgs = append(msgs, toMessage(rec))
				}
			})
			if idle && got == 0 {
				break
			}
		}
	}
	sort.Slice(msgs, func(i, j int) bool {
		if msgs[i].Partition != msgs[j].Partition {
			return msgs[i].Partition < msgs[j].Partition
		}
		return msgs[i].Offset < msgs[j].Offset
	})
	next, previous := map[int32]int64{}, map[int32]int64{}
	var newer, older bool
	for p, win := range windows {
		next[p], previous[p] = win.at, win.at
		if win.read > 0 {
			next[p], previous[p] = win.last+1, win.first
		}
		newer = newer || next[p] < win.end
		older = older || previous[p] > win.start
	}
	resp := map[string]any{"messages": msgs, "next": nil, "previous": nil}
	if newer {
		resp["next"] = formatCursor(next)
	}
	if older {
		resp["previous"] = formatCursor(previous)
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) profileOf(r *http.Request) (kafka.Profile, error) {
	c, err := s.Store.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		return kafka.Profile{}, err
	}
	return s.Clusters.Profile(c)
}

type produceRequest struct {
	Partition     *int32          `json:"partition"`
	Key           *string         `json:"key"`
	KeyEncoding   string          `json:"keyEncoding"`
	Value         json.RawMessage `json:"value"`
	ValueEncoding string          `json:"valueEncoding"`
	Headers       []MessageHeader `json:"headers"`
}

// record builds the record to produce; a null value makes a tombstone.
func (req produceRequest) record(topic string) (*kgo.Record, error) {
	rec := &kgo.Record{Topic: topic}
	if req.Key != nil {
		key, err := decodeBytes(*req.Key, req.KeyEncoding)
		if err != nil {
			return nil, fmt.Errorf("key: %w", err)
		}
		rec.Key = key
	}
	switch {
	case len(req.Value) == 0:
		return nil, errors.New("value is required; null produces a tombstone")
	case string(req.Value) != "null":
		var s string
		if err := json.Unmarshal(req.Value, &s); err != nil {
			return nil, errors.New("value must be a string or null")
		}
		value, err := decodeBytes(s, req.ValueEncoding)
		if err != nil {
			return nil, fmt.Errorf("value: %w", err)
		}
		rec.Value = value
	}
	if req.Partition != nil {
		rec.Partition = *req.Partition
	}
	for _, h := range req.Headers {
		value, err := decodeBytes(h.Value, h.Encoding)
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", h.Key, err)
		}
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: value})
	}
	return rec, nil
}

func (s *Server) handleProduce(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.capable(w, r, "browseMessages"); !ok {
		return
	}
	var req produceRequest
	if !Decode(w, r, &req) {
		return
	}
	rec, err := req.record(r.PathValue("topic"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	profile, err := s.profileOf(r)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	opts := []kgo.Opt{kgo.RequiredAcks(kgo.AllISRAcks())}
	if req.Partition != nil {
		opts = append(opts, kgo.RecordPartitioner(kgo.ManualPartitioner()))
	}
	producer, err := kafka.NewClient(profile, opts...)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	defer producer.Close()
	ctx, cancel := timeout(r)
	defer cancel()
	err = producer.KGO.ProduceSync(ctx, rec).FirstErr()
	s.Audit(r, "topic.produce", r.PathValue("id"), rec.Topic,
		map[string]any{"partition": rec.Partition, "bytes": len(rec.Value), "tombstone": rec.Value == nil}, err)
	if err != nil {
		kafkaErr(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"partition": rec.Partition, "offset": rec.Offset})
}
