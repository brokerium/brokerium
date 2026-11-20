// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

// Package problems finds risks in the settings and the placement of clusters (ADR 0023). Rules
// evaluate every snapshot of a cluster; a finding explains the risk, names the values behind it
// and offers fixes, which are operations the dashboard plans. Findings are kept in memory; the
// database keeps only their state: when a finding was first and last seen, who acknowledged it
// and until when it is muted.
package problems

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/brokerium/brokerium/internal/store"
)

// Severities of findings.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Kinds of the objects of findings.
const (
	ObjectCluster = "cluster"
	ObjectBroker  = "broker"
	ObjectTopic   = "topic"
	ObjectNode    = "node"
)

// Object is what a finding is about.
type Object struct {
	Kind string `json:"kind"`
	// ID is the cluster id, the broker id, the topic name or the id of the agent node.
	ID string `json:"id"`
	// Name is the name of a cluster or an agent node.
	Name string `json:"name,omitempty"`
}

// String renders the object as the problem_states table stores it.
func (o Object) String() string { return o.Kind + ":" + o.ID }

// Evidence is a value that a rule found.
type Evidence struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Fix is an operation that resolves a finding, with the parameters to plan it.
type Fix struct {
	Operation string `json:"operation"`
	Params    any    `json:"params"`
	Label     string `json:"label"`
}

// Finding is a risk that a rule found in a cluster. Its code and object identify it across
// snapshots; the first fix is the recommended one.
type Finding struct {
	Fingerprint    string     `json:"fingerprint"`
	Code           string     `json:"code"`
	Severity       string     `json:"severity"`
	Object         Object     `json:"object"`
	Title          string     `json:"title"`
	Explanation    string     `json:"explanation"`
	Evidence       []Evidence `json:"evidence"`
	Recommendation string     `json:"recommendation"`
	Fixes          []Fix      `json:"fixes"`
}

// Fingerprint identifies a finding by its code and its object.
func Fingerprint(code string, o Object) string {
	sum := sha256.Sum256([]byte(code + "\x00" + o.String()))
	return hex.EncodeToString(sum[:8])
}

func (f Finding) key() store.ProblemKey {
	return store.ProblemKey{Fingerprint: f.Fingerprint, Code: f.Code, Object: f.Object.String()}
}

func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}
