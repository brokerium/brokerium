// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bvaliev/brokerium/internal/agentgw"
	"github.com/bvaliev/brokerium/internal/provider/strimzi"
	"github.com/bvaliev/brokerium/internal/store"
)

var nodeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)

// NodePooler lists the node pools of Strimzi clusters.
type NodePooler interface {
	NodePools(ctx context.Context, c *store.Cluster) ([]strimzi.NodePool, error)
}

// NodesAPI exposes agent nodes, join tokens and Strimzi node pools.
type NodesAPI struct {
	Gateway       *agentgw.Gateway
	Pools         NodePooler
	PublicURL     string
	AgentEndpoint string
	srv           *Server
}

// Register adds the routes.
func (a *NodesAPI) Register(s *Server) {
	a.srv = s
	s.Route("GET /api/v1/clusters/{id}/node-pools", store.RoleViewer, a.handleNodePools)
	s.Route("GET /api/v1/nodes", store.RoleViewer, a.handleList)
	s.Route("POST /api/v1/join-tokens", store.RoleOperator, a.handleJoinToken)
	s.Route("DELETE /api/v1/nodes/{id}", store.RoleOperator, a.handleDelete)
	s.mux.HandleFunc("GET /api/v1/agent/install.sh", a.handleInstallScript)
}

// NodeView is a node with its connection state.
type NodeView struct {
	*store.Node
	Connected bool `json:"connected"`
}

// NodePoolView is a Strimzi node pool as the dashboard shows it.
type NodePoolView struct {
	Name     string   `json:"name"`
	Roles    []string `json:"roles"`
	Replicas int32    `json:"replicas"`
	Ready    int32    `json:"readyReplicas"`
	NodeIDs  []int32  `json:"nodeIds"`
	Settled  bool     `json:"settled"`
}

func (a *NodesAPI) handleNodePools(w http.ResponseWriter, r *http.Request) {
	c, err := a.srv.Store.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if c.Provider != store.ProviderStrimzi || a.Pools == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"pools": []NodePoolView{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	pools, err := a.Pools.NodePools(ctx, c)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "kubernetes_error", err.Error())
		return
	}
	out := make([]NodePoolView, 0, len(pools))
	for _, p := range pools {
		out = append(out, NodePoolView{Name: p.Metadata.Name, Roles: p.Spec.Roles, Replicas: p.Spec.Replicas,
			Ready: p.Status.Replicas, NodeIDs: p.Status.NodeIDs, Settled: p.Settled()})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"pools": out})
}

func (a *NodesAPI) handleList(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.srv.Store.ListNodes(r.Context(), r.URL.Query().Get("clusterId"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	out := make([]NodeView, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, NodeView{Node: n, Connected: a.Gateway.Connected(n.ID)})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

type joinTokenRequest struct {
	NodeName   string  `json:"nodeName"`
	ClusterID  *string `json:"clusterId"`
	TTLMinutes int     `json:"ttlMinutes"`
}

func (a *NodesAPI) handleJoinToken(w http.ResponseWriter, r *http.Request) {
	var req joinTokenRequest
	if !Decode(w, r, &req) {
		return
	}
	if !nodeNameRe.MatchString(req.NodeName) {
		WriteError(w, http.StatusBadRequest, "bad_request", "nodeName must be lowercase letters, digits, dots and dashes")
		return
	}
	if req.ClusterID != nil && *req.ClusterID == "" {
		req.ClusterID = nil
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if ttl <= 0 || ttl > 7*24*time.Hour {
		ttl = time.Hour
	}
	token, expires, err := a.Gateway.NewJoinToken(r.Context(), req.NodeName, req.ClusterID, ttl, UserFrom(r.Context()).Username)
	a.srv.Audit(r, "node.join-token", deref(req.ClusterID), req.NodeName, map[string]any{"ttlMinutes": int(ttl.Minutes())}, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	cmd := fmt.Sprintf("curl -fsSL %s/api/v1/agent/install.sh | sudo BROKERIUM_AGENT_SERVER=%s BROKERIUM_AGENT_TOKEN=%s sh",
		strings.TrimRight(a.PublicURL, "/"), a.AgentEndpoint, token)
	WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "expiresAt": expires, "command": cmd})
}

func (a *NodesAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := a.srv.Store.GetNode(r.Context(), id)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if a.Gateway.Connected(id) && n.ClusterID != nil {
		WriteError(w, http.StatusConflict, "node_in_use", "the node is connected and assigned to a cluster; decommission it first")
		return
	}
	err = a.srv.Store.DeleteNode(r.Context(), id)
	a.srv.Audit(r, "node.delete", deref(n.ClusterID), n.Name, nil, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// installScript installs brokerium-agent as a systemd service on a Linux host.
const installScript = `#!/bin/sh
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# Installs brokerium-agent as a systemd service. Requires BROKERIUM_AGENT_SERVER and BROKERIUM_AGENT_TOKEN.
set -eu
: "${BROKERIUM_AGENT_SERVER:?set BROKERIUM_AGENT_SERVER}"
: "${BROKERIUM_AGENT_TOKEN:?set BROKERIUM_AGENT_TOKEN}"
VERSION="${BROKERIUM_AGENT_VERSION:-latest}"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
if [ "$VERSION" = latest ]; then
  URL="https://github.com/bvaliev/brokerium/releases/latest/download/brokerium-agent-linux-${ARCH}"
else
  URL="https://github.com/bvaliev/brokerium/releases/download/${VERSION}/brokerium-agent-linux-${ARCH}"
fi
id kafka >/dev/null 2>&1 || useradd --system --home-dir /var/lib/brokerium-agent --shell /usr/sbin/nologin kafka
install -d -o kafka -g kafka /var/lib/brokerium-agent /opt/brokerium/kafka /var/lib/kafka/data
curl -fsSL "$URL" -o /usr/local/bin/brokerium-agent
chmod 0755 /usr/local/bin/brokerium-agent
cat > /etc/systemd/system/brokerium-agent.service <<EOF
[Unit]
Description=Brokerium agent for Apache Kafka
After=network-online.target
Wants=network-online.target

[Service]
User=kafka
Environment=BROKERIUM_AGENT_SERVER=${BROKERIUM_AGENT_SERVER}
Environment=BROKERIUM_AGENT_TOKEN=${BROKERIUM_AGENT_TOKEN}
ExecStart=/usr/local/bin/brokerium-agent run
Restart=always
RestartSec=5
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now brokerium-agent.service
echo "brokerium-agent installed; it enrolls with the server on first start."
`

func (a *NodesAPI) handleInstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(installScript))
}
