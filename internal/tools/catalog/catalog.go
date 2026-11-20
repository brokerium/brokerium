// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/brokerium/brokerium/internal/kafkaconfig"
)

type describedConfig = kmsg.DescribeConfigsResponseResourceConfig

// Known differences between Kafka and its documentation, which get no note.
var (
	// jvmDefaults depend on the JVM: Java 11 and later get TLSv1.3. The documentation of some
	// releases was built on Java 8 and shows TLSv1.2.
	jvmDefaults = map[string]bool{"ssl.enabled.protocols": true, "ssl.protocol": true}
	// dynamicOnly broker keys are in the documentation from 4.3, but Kafka reports them only
	// once they are set. They are the throttles of reassignments, which the operations engine
	// owns, so the catalog leaves them out.
	dynamicOnly = map[string]bool{
		"follower.replication.throttled.rate":            true,
		"leader.replication.throttled.rate":              true,
		"replica.alter.log.dirs.io.max.bytes.per.second": true,
	}
)

// buildCatalog turns what the node and the documentation report into the catalog of a release.
// Notes are differences between Kafka and its documentation.
func buildCatalog(v version, d docs, broker, topic []describedConfig) (*kafkaconfig.Catalog, []string, error) {
	c := &kafkaconfig.Catalog{Line: v.line().String(), Version: v.String(), Docs: d.source}
	var (
		notes []string
		errs  []error
	)
	for _, cfg := range broker {
		if err := checkBrokerSource(cfg); err != nil {
			errs = append(errs, err)
			continue
		}
		k, err := newKey(cfg, brokerDefault(cfg))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		doc, documented := d.broker[k.Name]
		k.ReadOnly = cfg.ReadOnly
		k.Importance = doc.importance
		var note string
		k.UpdateMode, note = updateMode(k, doc, documented)
		if note != "" {
			notes = append(notes, note)
		}
		notes = append(notes, compareDocs(kafkaconfig.Broker, k, doc, documented)...)
		c.Broker = append(c.Broker, k)
	}
	for _, cfg := range topic {
		if cfg.Source != kmsg.ConfigSourceDefaultConfig && !bootstrapDefault(cfg) {
			errs = append(errs, fmt.Errorf("topic key %s comes from %s, not from the defaults", cfg.Name, cfg.Source))
			continue
		}
		k, err := newKey(cfg, topicDefault(cfg))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		doc, documented := d.topic[k.Name]
		k.Importance = doc.importance
		if !documented && d.topic != nil {
			notes = append(notes, fmt.Sprintf("topic key %s is not in the documentation", k.Name))
		}
		notes = append(notes, compareDocs(kafkaconfig.Topic, k, doc, documented)...)
		c.Topic = append(c.Topic, k)
	}
	byName := func(a, b kafkaconfig.Key) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(c.Broker, byName)
	slices.SortFunc(c.Topic, byName)
	notes = append(notes, unreported(kafkaconfig.Broker, d.broker, c.Broker)...)
	notes = append(notes, unreported(kafkaconfig.Topic, d.topic, c.Topic)...)
	return c, notes, errors.Join(errs...)
}

// nodeKeys are the broker keys the configuration of the node sets; KRaft copies node.id into
// broker.id.
func nodeKeys() map[string]bool {
	out := map[string]bool{"broker.id": true}
	for _, kv := range nodeConfig {
		k, _, _ := strings.Cut(kv, "=")
		out[k] = true
	}
	return out
}

// checkBrokerSource makes sure that a broker key has the value of the node configuration or the
// default: a key set anywhere else, for example by the image, could change what topics inherit.
func checkBrokerSource(cfg describedConfig) error {
	switch {
	case cfg.Source == kmsg.ConfigSourceDefaultConfig || bootstrapDefault(cfg):
		return nil
	case cfg.Source == kmsg.ConfigSourceStaticBrokerConfig && nodeKeys()[cfg.Name]:
		return nil
	case cfg.Source == kmsg.ConfigSourceStaticBrokerConfig:
		return fmt.Errorf("broker key %s is set in server.properties, which must set only the keys of the node", cfg.Name)
	}
	return fmt.Errorf("broker key %s comes from %s, not from the defaults", cfg.Name, cfg.Source)
}

// bootstrapDefault reports whether a key is a cluster-wide dynamic value equal to its default.
// From 4.1 a new cluster records min.insync.replicas that way, because eligible leader replicas
// need a cluster-wide value.
func bootstrapDefault(cfg describedConfig) bool {
	if cfg.Source != kmsg.ConfigSourceDynamicDefaultBrokerConfig || cfg.Value == nil {
		return false
	}
	for _, s := range cfg.ConfigSynonyms {
		if s.Source == kmsg.ConfigSourceDefaultConfig {
			return s.Value != nil && *s.Value == *cfg.Value
		}
	}
	return false
}

func newKey(cfg describedConfig, def *string) (kafkaconfig.Key, error) {
	if cfg.ConfigType == kmsg.ConfigTypeUnknown {
		return kafkaconfig.Key{}, fmt.Errorf("key %s has no type", cfg.Name)
	}
	k := kafkaconfig.Key{
		Name:      cfg.Name,
		Type:      strings.ToLower(cfg.ConfigType.String()),
		Default:   def,
		Sensitive: cfg.IsSensitive,
	}
	if cfg.Documentation != nil {
		k.Documentation = *cfg.Documentation
	}
	return k, nil
}

// brokerDefault is the value of the DEFAULT_CONFIG synonym of a broker key. The value Kafka
// reports reflects server.properties; a key without the synonym has no default.
func brokerDefault(cfg describedConfig) *string {
	for _, s := range cfg.ConfigSynonyms {
		if s.Source == kmsg.ConfigSourceDefaultConfig && s.Name == cfg.Name {
			return s.Value
		}
	}
	return nil
}

// topicDefault is the default of a topic key: the DEFAULT_CONFIG synonym, which names the
// broker key the topic inherits from, or else the value of the new topic. Kafka drops the
// synonyms of some keys, such as retention.ms, whose broker key (log.retention.ms) has no
// default and falls back to another one (log.retention.hours); the value of the topic is the
// default then, because the node sets no key that topics inherit.
func topicDefault(cfg describedConfig) *string {
	for _, s := range cfg.ConfigSynonyms {
		if s.Source == kmsg.ConfigSourceDefaultConfig {
			return s.Value
		}
	}
	return cfg.Value
}

// updateMode is the update mode the documentation gives a broker key, as long as it agrees with
// whether Kafka reports the key read-only. Otherwise Kafka decides: a read-only key is
// read-only, and a dynamic key without a dynamic mode in the documentation is per-broker, the
// update every dynamic key accepts.
func updateMode(k kafkaconfig.Key, doc docKey, documented bool) (kafkaconfig.UpdateMode, string) {
	mode := kafkaconfig.UpdateMode(doc.updateMode)
	dynamic := mode == kafkaconfig.PerBroker || mode == kafkaconfig.ClusterWide
	switch {
	case !documented && k.ReadOnly:
		return kafkaconfig.ReadOnly, fmt.Sprintf("broker key %s is not in the documentation; Kafka reports it read-only", k.Name)
	case !documented:
		return kafkaconfig.PerBroker, fmt.Sprintf("broker key %s is not in the documentation; Kafka reports it dynamic, taken as per-broker", k.Name)
	case k.ReadOnly && mode != kafkaconfig.ReadOnly:
		return kafkaconfig.ReadOnly, fmt.Sprintf("broker key %s: the documentation says %q, Kafka reports it read-only", k.Name, mode)
	case !k.ReadOnly && !dynamic:
		return kafkaconfig.PerBroker, fmt.Sprintf("broker key %s: the documentation says %q, Kafka reports it dynamic, taken as per-broker", k.Name, mode)
	}
	return mode, ""
}

// docSuffix is the readable form the documentation adds to sizes and durations, such as
// "1073741824 (1 gibibyte)".
var docSuffix = regexp.MustCompile(` \([^()]*\)$`)

// compareDocs notes where the documentation disagrees with Kafka about the type or the default.
func compareDocs(scope kafkaconfig.Scope, k kafkaconfig.Key, doc docKey, documented bool) []string {
	if !documented {
		return nil
	}
	var notes []string
	if doc.typ != k.Type {
		notes = append(notes, fmt.Sprintf("%s key %s: the documentation says type %s, Kafka %s", scope, k.Name, doc.typ, k.Type))
	}
	// The documentation renders no default as an empty cell, null as null and an empty string as
	// "".
	var def *string
	switch v := docSuffix.ReplaceAllString(doc.def, ""); v {
	case "", "null":
	case `""`:
		def = new(string)
	default:
		def = &v
	}
	same := def == nil && k.Default == nil || def != nil && k.Default != nil && *def == *k.Default
	if !same && !jvmDefaults[k.Name] {
		notes = append(notes, fmt.Sprintf("%s key %s: the documentation says default %s, Kafka %s", scope, k.Name, show(def), show(k.Default)))
	}
	return notes
}

// unreported notes keys of the documentation that Kafka does not report.
func unreported(scope kafkaconfig.Scope, doc map[string]docKey, keys []kafkaconfig.Key) []string {
	var notes []string
	for name := range doc {
		if !dynamicOnly[name] && !slices.ContainsFunc(keys, func(k kafkaconfig.Key) bool { return k.Name == name }) {
			notes = append(notes, fmt.Sprintf("the documentation lists the %s key %s, which Kafka does not report", scope, name))
		}
	}
	slices.Sort(notes)
	return notes
}

func show(s *string) string {
	if s == nil {
		return "none"
	}
	return fmt.Sprintf("%q", *s)
}
