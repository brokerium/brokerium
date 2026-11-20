// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/ops"
)

// Operation types of the problems feature.
const (
	OpChangeReplicationFactor = "change-replication-factor"
	OpAlterTopicConfigs       = "alter-topic-configs"
	OpAlignBrokerConfigs      = "align-broker-configs"
)

// Step kinds of the problems feature.
const (
	KindAlterTopicConfigs   = "alter-topic-configs"
	KindSetClusterConfigs   = "set-cluster-broker-configs"
	KindDeleteBrokerConfigs = "delete-broker-configs"
)

// OperationTypes returns the operation types of the problems feature, the fixes of its findings.
func OperationTypes() []ops.OpType {
	return []ops.OpType{
		{Type: OpChangeReplicationFactor, Title: "Change replication factor", Scope: "cluster", Requires: []string{"reassign"},
			Feature: edition.Problems, Plan: planChangeReplicationFactor,
			Description: "Raise or lower the replication factor of topics, internal topics included, with throttled reassignments that spread replicas over racks."},
		{Type: OpAlterTopicConfigs, Title: "Change topic configs", Scope: "cluster", Requires: []string{"manageConfigs"},
			Feature: edition.Problems, Plan: planAlterTopicConfigs,
			Description: "Set or remove configs of topics, such as min.insync.replicas, after the brokers validated the change."},
		{Type: OpAlignBrokerConfigs, Title: "Align broker configs", Scope: "cluster", Requires: []string{"manageConfigs"},
			Feature: edition.Problems, Plan: planAlignBrokerConfigs,
			Description: "Give dynamic broker configs one cluster-wide value and remove the values of single brokers that differ."},
	}
}

// StepKinds returns the step kinds of the problems feature.
func StepKinds() map[string]ops.StepFunc {
	return map[string]ops.StepFunc{
		KindAlterTopicConfigs:   stepAlterTopicConfigs,
		KindSetClusterConfigs:   stepSetClusterConfigs,
		KindDeleteBrokerConfigs: stepDeleteBrokerConfigs,
	}
}

// decodeParams reads the parameters of an operation and refuses unknown fields.
func decodeParams[T any](raw json.RawMessage) (T, error) {
	var v T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("invalid params: %w", err)
	}
	return v, nil
}
