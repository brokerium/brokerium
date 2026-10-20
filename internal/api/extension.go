// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"net/http"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
)

// Helpers for extensions in other packages, such as the features of Brokerium Enterprise.

// Capable answers 403 when the cluster of the request lacks the capability; otherwise it returns
// the Kafka client and the view of the cluster.
func (s *Server) Capable(w http.ResponseWriter, r *http.Request, capability string) (*kafka.Client, *cluster.View, bool) {
	return s.capable(w, r, capability)
}

// Profile returns the connection profile of the cluster of the request, for clients with their
// own options such as consumers.
func (s *Server) Profile(r *http.Request) (kafka.Profile, error) {
	return s.profileOf(r)
}

// KafkaError answers with the status that matches an error from Kafka.
func KafkaError(w http.ResponseWriter, err error) {
	kafkaErr(w, err)
}

// RequestTimeout bounds the Kafka requests that serve an API request.
func RequestTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return timeout(r)
}
