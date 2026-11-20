// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/agentcfg"
	"github.com/brokerium/brokerium/internal/agentgw"
	"github.com/brokerium/brokerium/internal/provider/strimzi"
	"github.com/brokerium/brokerium/internal/store"
)

const nodeNameRule = "1-63 letters, digits, '.', '_' and '-'"

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
	// Proxy and NoProxy go into the install command as HTTPS_PROXY and NO_PROXY.
	Proxy, NoProxy string
	// Transport of the install command. With insecure, agents dial InsecureEndpoint with the
	// TLS mode InsecureTLS (ADR 0017).
	Transport, InsecureEndpoint, InsecureTLS string
	// CertificateWarning is how long before expiry an agent certificate counts as expiring.
	CertificateWarning time.Duration
	srv                *Server
}

// Register adds the routes.
func (a *NodesAPI) Register(s *Server) {
	a.srv = s
	s.Route("GET /api/v1/clusters/{id}/node-pools", store.RoleViewer, a.handleNodePools)
	s.Route("GET /api/v1/nodes", store.RoleViewer, a.handleList)
	s.Route("POST /api/v1/join-tokens", store.RoleOperator, a.handleJoinToken)
	s.Route("PATCH /api/v1/nodes/{id}", store.RoleOperator, a.handleUpdate)
	s.Route("DELETE /api/v1/nodes/{id}", store.RoleOperator, a.handleDelete)
	s.mux.HandleFunc("GET /api/v1/agent/install.sh", a.handleInstallScript)
	s.mux.HandleFunc("GET /api/v1/agent/uninstall.sh", a.handleUninstallScript)
}

// NodeView is a node with its connection state.
type NodeView struct {
	*store.Node
	Connected bool `json:"connected"`
	// CertificateState of the agent certificate: ok, expiring (not renewed in time), expired or
	// empty when unknown.
	CertificateState string `json:"certificateState"`
}

func certificateState(n *store.Node, now time.Time, warning time.Duration) string {
	switch {
	case n.CertNotAfter == nil:
		return ""
	case !now.Before(*n.CertNotAfter):
		return "expired"
	case n.CertNotAfter.Sub(now) <= warning:
		return "expiring"
	default:
		return "ok"
	}
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
		out = append(out, a.view(n))
	}
	WriteJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

func (a *NodesAPI) view(n *store.Node) NodeView {
	return NodeView{Node: n, Connected: a.Gateway.Connected(n.ID), CertificateState: certificateState(n, time.Now(), a.CertificateWarning)}
}

// joinTokenRequest creates a join token for a new node, named nodeName or after the host name
// the agent reports, or for enrolling the existing node nodeId again.
type joinTokenRequest struct {
	NodeName   string  `json:"nodeName"`
	ClusterID  *string `json:"clusterId"`
	TTLMinutes int     `json:"ttlMinutes"`
	NodeID     string  `json:"nodeId"`
}

func (req *joinTokenRequest) validate() error {
	if req.ClusterID != nil && *req.ClusterID == "" {
		req.ClusterID = nil
	}
	switch {
	case req.NodeID != "" && (req.NodeName != "" || req.ClusterID != nil):
		return errors.New("nodeId enrolls an existing node again, which keeps its name and cluster; leave nodeName and clusterId empty")
	case req.NodeName != "" && !agentgw.ValidNodeName(req.NodeName):
		return errors.New("nodeName must be " + nodeNameRule)
	}
	return nil
}

func (a *NodesAPI) handleJoinToken(w http.ResponseWriter, r *http.Request) {
	var req joinTokenRequest
	if !Decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	tok := store.JoinToken{NodeName: req.NodeName, ClusterID: req.ClusterID, CreatedBy: UserFrom(r.Context()).Username}
	clusterID, target, detail := deref(req.ClusterID), req.NodeName, map[string]any{}
	if req.NodeID != "" {
		n, err := a.srv.Store.GetNode(r.Context(), req.NodeID)
		if err != nil {
			WriteStoreError(w, err)
			return
		}
		tok.NodeID = &n.ID
		clusterID, target, detail["nodeId"] = deref(n.ClusterID), n.Name, n.ID
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if ttl <= 0 || ttl > 7*24*time.Hour {
		ttl = time.Hour
	}
	detail["ttlMinutes"] = int(ttl.Minutes())
	token, expires, err := a.Gateway.NewJoinToken(r.Context(), tok, ttl)
	a.srv.Audit(r, "node.join-token", clusterID, target, detail, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "expiresAt": expires, "command": a.installCommand(token)})
}

// nodeUpdate changes the name, advertised host or rack of a node; absent fields stay.
type nodeUpdate struct {
	Name           *string `json:"name,omitempty"`
	AdvertisedHost *string `json:"advertisedHost,omitempty"`
	Rack           *string `json:"rack,omitempty"`
}

func (u nodeUpdate) validate() error {
	switch {
	case u.Name == nil && u.AdvertisedHost == nil && u.Rack == nil:
		return errors.New("give name, advertisedHost or rack")
	case u.Name != nil && !agentgw.ValidNodeName(*u.Name):
		return errors.New("name must be " + nodeNameRule)
	}
	if u.AdvertisedHost != nil && *u.AdvertisedHost != "" {
		if err := agentcfg.ValidateHost(*u.AdvertisedHost); err != nil {
			return fmt.Errorf("advertisedHost: %w", err)
		}
	}
	if u.Rack != nil {
		return agentcfg.ValidateRack(*u.Rack)
	}
	return nil
}

// apply changes n; an empty advertised host goes back to the host name the agent reported. A
// node that runs Kafka keeps an advertised host that its listeners can advertise.
func (u nodeUpdate) apply(n *store.Node) error {
	if u.Name != nil {
		n.Name = *u.Name
	}
	if u.AdvertisedHost != nil {
		n.AdvertisedHost = cmp.Or(*u.AdvertisedHost, n.Hostname)
		if n.Role != "" {
			if err := agentcfg.ValidateHost(n.AdvertisedHost); err != nil {
				return fmt.Errorf("advertisedHost: the node runs Kafka and the host name its agent reported cannot be advertised: %w", err)
			}
		}
	}
	if u.Rack != nil {
		n.Rack = *u.Rack
	}
	return nil
}

func (a *NodesAPI) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req nodeUpdate
	if !Decode(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	n, err := a.srv.Store.GetNode(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if n.ClusterID != nil && ((req.Name != nil && *req.Name != n.Name) || req.AdvertisedHost != nil) {
		c, err := a.srv.Store.GetCluster(r.Context(), *n.ClusterID)
		if err == nil && c.Provider == store.ProviderDocker {
			WriteError(w, http.StatusBadRequest, "bad_request",
				"nodes of Docker clusters are named after their containers, which their listeners advertise")
			return
		}
	}
	before := n.Name
	if err := req.apply(n); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	err = a.srv.Store.UpdateNodeDetails(r.Context(), n)
	a.srv.Audit(r, "node.update", deref(n.ClusterID), before, req, err)
	switch {
	case errors.Is(err, store.ErrConflict):
		WriteError(w, http.StatusConflict, "conflict", fmt.Sprintf("a node named %s already exists", n.Name))
		return
	case err != nil:
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, a.view(n))
}

// installCommand is the command that installs the agent on a host. sudo drops the proxy
// variables of the caller, so they are passed to curl and to the script explicitly.
func (a *NodesAPI) installCommand(token string) string {
	proxy := a.proxyVariables()
	server := "BROKERIUM_AGENT_SERVER=" + a.AgentEndpoint
	if a.Transport == store.TransportInsecure {
		server = fmt.Sprintf("BROKERIUM_AGENT_TRANSPORT=insecure BROKERIUM_AGENT_TLS=%s BROKERIUM_AGENT_SERVER=%s",
			a.InsecureTLS, a.InsecureEndpoint)
	}
	return fmt.Sprintf("%scurl -fsSL %s/api/v1/agent/install.sh | sudo %s%s BROKERIUM_AGENT_TOKEN=%s sh",
		proxy, strings.TrimRight(a.PublicURL, "/"), proxy, server, token)
}

// uninstallCommand is the command that removes the agent from a host; its options follow "--".
// The script downloads nothing, so only curl needs the proxy.
func (a *NodesAPI) uninstallCommand() string {
	return fmt.Sprintf("%scurl -fsSL %s/api/v1/agent/uninstall.sh | sudo sh -s --", a.proxyVariables(),
		strings.TrimRight(a.PublicURL, "/"))
}

// proxyVariables sets HTTPS_PROXY and NO_PROXY for one command, or is empty without a proxy.
func (a *NodesAPI) proxyVariables() string {
	if a.Proxy == "" {
		return ""
	}
	proxy := "HTTPS_PROXY=" + shellQuote(a.Proxy) + " "
	if a.NoProxy != "" {
		proxy += "NO_PROXY=" + shellQuote(a.NoProxy) + " "
	}
	return proxy
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (a *NodesAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	n, err := deleteNode(r.Context(), a.srv.Store, a.Gateway, a.stillInCluster, r.PathValue("id"))
	var inUse *nodeInUseError
	switch {
	case errors.As(err, &inUse):
		WriteError(w, http.StatusConflict, "node_in_use", err.Error())
		return
	case n == nil:
		WriteStoreError(w, err)
		return
	}
	a.srv.Audit(r, "node.delete", deref(n.ClusterID), n.Name, nil, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"uninstallCommand": a.uninstallCommand()})
}

// nodeRecords is the part of the store that deleting a node uses.
type nodeRecords interface {
	GetNode(ctx context.Context, id string) (*store.Node, error)
	DeleteNode(ctx context.Context, id string) error
}

// sessionCloser ends the session of the agent of a deleted node.
type sessionCloser interface {
	NodeDeleted(nodeID string)
}

// memberCheck reports whether a cluster still knows Kafka node id.
type memberCheck func(ctx context.Context, clusterID string, id int32) (bool, error)

// nodeInUseError refuses to delete a node that runs a Kafka node of a cluster: the node would
// keep running Kafka for the cluster without anyone managing it.
type nodeInUseError struct{ reason string }

func (e *nodeInUseError) Error() string { return e.reason }

// deleteNode deletes the node id and ends the session of its agent. A node that runs a broker or
// a controller is decommissioned first, which stops Kafka on it and releases it from its
// cluster. Brokerium cannot decommission combined nodes; they are taken out of their cluster
// with the Kafka tools, and their node is deleted once the cluster no longer knows them. The
// node is nil when it cannot be read.
func deleteNode(ctx context.Context, nodes nodeRecords, sessions sessionCloser, inCluster memberCheck, id string) (*store.Node, error) {
	n, err := nodes.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := deletable(ctx, n, inCluster); err != nil {
		return n, err
	}
	if err := nodes.DeleteNode(ctx, id); err != nil {
		return n, err
	}
	sessions.NodeDeleted(id)
	return n, nil
}

// deletable refuses a node that holds a Kafka role in a cluster, unless it is a combined node
// that the cluster no longer knows.
func deletable(ctx context.Context, n *store.Node, inCluster memberCheck) error {
	switch n.Role {
	case "":
		return nil
	case "broker":
		return &nodeInUseError{fmt.Sprintf("node %s runs %s of a cluster; decommission it first with drain-broker "+
			"(decommission) or replace-broker, which stop Kafka on the host and release it", n.Name, held(n))}
	case "controller":
		return &nodeInUseError{fmt.Sprintf("node %s runs %s of a cluster; decommission it first with remove-controller, "+
			"which stops Kafka on the host and releases it", n.Name, held(n))}
	}
	if n.ClusterID == nil || n.KafkaNodeID == nil {
		return &nodeInUseError{fmt.Sprintf("node %s runs %s, and its cluster cannot be asked about it", n.Name, held(n))}
	}
	known, err := inCluster(ctx, *n.ClusterID, *n.KafkaNodeID)
	switch {
	case err != nil:
		return &nodeInUseError{fmt.Sprintf("node %s runs %s; checking whether its cluster still has it failed: %v",
			n.Name, held(n), err)}
	case known:
		return &nodeInUseError{fmt.Sprintf("node %s runs %s, which its cluster still has as a broker, a voter or an observer; "+
			"Brokerium does not decommission combined nodes: take it out of the quorum, stop Kafka and unregister its broker "+
			"first (%s). A stopped node stays an observer for a few minutes", n.Name, held(n), combinedNodesGuide)}
	}
	return nil
}

// held names the Kafka node that node n runs, such as "broker 101" or "combined node 1".
func held(n *store.Node) string {
	role := n.Role
	if role == "combined" {
		role = "combined node"
	}
	if n.KafkaNodeID == nil {
		return role
	}
	return fmt.Sprintf("%s %d", role, *n.KafkaNodeID)
}

// combinedNodesGuide explains how combined nodes leave their cluster.
const combinedNodesGuide = "https://brokerium.kaufen/docs/guide/clusters/#combined-nodes"

// stillInCluster reports whether a cluster still knows Kafka node id: as a registered broker,
// fenced or not, or as a voter or an observer of the quorum, which every running node is.
func (a *NodesAPI) stillInCluster(ctx context.Context, clusterID string, id int32) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, _, err := a.srv.Clusters.Client(ctx, clusterID)
	if err != nil {
		return false, err
	}
	info, err := client.DescribeCluster(ctx, true)
	if err != nil {
		return false, err
	}
	for _, b := range info.Brokers {
		if b.NodeID == id {
			return true, nil
		}
	}
	q, err := client.DescribeQuorum(ctx)
	if err != nil {
		return false, err
	}
	_, voter := q.Voter(id)
	_, observer := q.Observer(id)
	return voter || observer, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// installTemplate installs brokerium-agent as a systemd service on a Linux host.
const installTemplate = `#!/bin/sh
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# Installs brokerium-agent as a systemd service under its own account, brokerium-agent. Kafka runs
# as its own unit ({{.KafkaUnit}}) under the kafka account, so restarting the agent never
# stops Kafka. The agent reaches the Kafka directories through the kafka group, and a sudo rule
# lets it start, stop and restart that unit and nothing else. Running the script again on a host
# whose agent runs as root moves the agent to its own account; a running Kafka keeps running.
#
#   BROKERIUM_AGENT_SERVER, BROKERIUM_AGENT_TOKEN  required (Nodes page of the dashboard)
#   BROKERIUM_AGENT_VERSION                        release tag, default: latest
#   BROKERIUM_AGENT_URL                            download URL of the binary (air-gapped installs)
#   BROKERIUM_AGENT_SHA256                         expected SHA-256 of that binary
#   BROKERIUM_AGENT_KAFKA_DATA_DIR                 log.dirs of new nodes, default {{.DataDir}}
#   BROKERIUM_AGENT_KAFKA_CONFIG                   adopt a running node: its server.properties
#   BROKERIUM_AGENT_PREINSTALLED                   and its Kafka installation directory
#   HTTPS_PROXY, NO_PROXY                          HTTP CONNECT proxy for the downloads and for the
#                                                  agent; pass them after sudo, which drops them
#   BROKERIUM_AGENT_TRANSPORT                      mtls (default) or insecure: signed messages for
#                                                  a server behind a TLS-terminating proxy
#   BROKERIUM_AGENT_TLS                            insecure transport: system (default), skip-verify
#                                                  or none (plaintext)
#   BROKERIUM_AGENT_CA_FILE                        PEM file of extra CAs to verify the proxy with
set -eu
: "${BROKERIUM_AGENT_SERVER:?set BROKERIUM_AGENT_SERVER}"
: "${BROKERIUM_AGENT_TOKEN:?set BROKERIUM_AGENT_TOKEN}"
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
[ -z "${BROKERIUM_AGENT_CA_FILE:-}" ] || [ -r "$BROKERIUM_AGENT_CA_FILE" ] ||
  { echo "BROKERIUM_AGENT_CA_FILE $BROKERIUM_AGENT_CA_FILE is not readable" >&2; exit 1; }
SYSTEMCTL=$(command -v systemctl) || { echo "systemctl not found: the agent runs as a systemd service" >&2; exit 1; }
if ! command -v sudo >/dev/null || ! command -v visudo >/dev/null || [ ! -d /etc/sudoers.d ]; then
  echo "sudo is not installed: install it (with /etc/sudoers.d) and run this again; the agent starts and stops Kafka through a sudo rule" >&2
  exit 1
fi
VERSION="${BROKERIUM_AGENT_VERSION:-latest}"
STATE={{.StateDir}}
KAFKA_DIR={{.KafkaDir}}
LOG_DIR="$STATE/logs"
DATA_DIR="${BROKERIUM_AGENT_KAFKA_DATA_DIR:-{{.DataDir}}}"
KAFKA_CONFIG="${BROKERIUM_AGENT_KAFKA_CONFIG:-$STATE/server.properties}"
for p in "$DATA_DIR" "$KAFKA_CONFIG"; do
  # Both paths go into unit files.
  case "$p" in
    [!/]*|*[!A-Za-z0-9._/@:+-]*) echo "unsupported path $p: use an absolute path of letters, digits and ._/@:+-" >&2; exit 1 ;;
  esac
done
PROXY="${HTTPS_PROXY:-${https_proxy:-}}"
NOPROXY="${NO_PROXY:-${no_proxy:-}}"
if [ -n "$PROXY" ]; then
  export HTTPS_PROXY="$PROXY" https_proxy="$PROXY"
  [ -z "$NOPROXY" ] || export NO_PROXY="$NOPROXY" no_proxy="$NOPROXY"
fi
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
if [ -n "${BROKERIUM_AGENT_URL:-}" ]; then
  URL="$BROKERIUM_AGENT_URL"
elif [ "$VERSION" = latest ]; then
  URL="https://github.com/brokerium/brokerium/releases/latest/download/brokerium-agent-linux-${ARCH}"
else
  URL="https://github.com/brokerium/brokerium/releases/download/${VERSION}/brokerium-agent-linux-${ARCH}"
fi
AGENT_UNIT={{.AgentUnitFile}}
KAFKA_UNIT={{.KafkaUnitFile}}
# On a host whose agent ran as root, the files of the agent move to its account below.
CONVERT=1
if grep -qx 'User=brokerium-agent' "$AGENT_UNIT" 2>/dev/null; then CONVERT=0; fi
getent group kafka >/dev/null || groupadd --system kafka
id kafka >/dev/null 2>&1 || useradd --system --gid kafka --home-dir /var/lib/kafka --shell /usr/sbin/nologin kafka
getent group brokerium-agent >/dev/null || groupadd --system brokerium-agent
id brokerium-agent >/dev/null 2>&1 ||
  useradd --system --gid brokerium-agent --home-dir "$STATE" --shell /usr/sbin/nologin brokerium-agent
usermod -a -G kafka brokerium-agent
# The agent unit sets ProtectHome, so the file must also live outside /home, /root and /run/user.
case "${BROKERIUM_AGENT_CA_FILE:-}" in
  /home/* | /root/* | /run/user/*)
    echo "BROKERIUM_AGENT_CA_FILE must be outside /home, /root and /run/user, for example in /etc" >&2; exit 1 ;;
esac
[ -z "${BROKERIUM_AGENT_CA_FILE:-}" ] || runuser -u brokerium-agent -- test -r "$BROKERIUM_AGENT_CA_FILE" ||
  { echo "BROKERIUM_AGENT_CA_FILE $BROKERIUM_AGENT_CA_FILE is not readable by the brokerium-agent account" >&2; exit 1; }
# sudo skips files whose names contain a dot, so the rule takes effect only once visudo accepted it.
RULE=$(mktemp {{.SudoersTemp}})
cat > "$RULE" <<EOF
Defaults:brokerium-agent !requiretty
brokerium-agent ALL=(root) NOPASSWD: $SYSTEMCTL start {{.KafkaUnit}}, $SYSTEMCTL stop {{.KafkaUnit}}, $SYSTEMCTL restart {{.KafkaUnit}}
EOF
chmod 0440 "$RULE"
if ! visudo -cf "$RULE" >/dev/null; then
  rm -f "$RULE"
  echo "visudo rejected the sudo rule for brokerium-agent; the agent was not installed or changed" >&2
  exit 1
fi
mv -f "$RULE" {{.SudoersFile}}
if ! sudo -n -l -U brokerium-agent "$SYSTEMCTL" restart {{.KafkaUnit}} >/dev/null 2>&1; then
  echo "sudo ignores {{.SudoersFile}} (does /etc/sudoers include /etc/sudoers.d?); the agent was not installed or changed" >&2
  exit 1
fi
curl -fsSL "$URL" -o {{.Binary}}.new
if [ -n "${BROKERIUM_AGENT_URL:-}" ]; then
  WANT="${BROKERIUM_AGENT_SHA256:-}"
else
  # Release binaries are checked against the SHA256SUMS of the same release.
  WANT=$(curl -fsSL "${URL%/*}/SHA256SUMS" | awk -v f="brokerium-agent-linux-${ARCH}" '$2 == f {print $1}')
  [ -n "$WANT" ] || { echo "no checksum for brokerium-agent-linux-${ARCH} in the release" >&2; exit 1; }
fi
if [ -n "$WANT" ] && [ "$(sha256sum {{.Binary}}.new | cut -d' ' -f1)" != "$WANT" ]; then
  rm -f {{.Binary}}.new
  echo "checksum mismatch for $URL" >&2
  exit 1
fi
chmod 0755 {{.Binary}}.new
# Only the agent restarts. If a later step fails, it starts again with the unit systemd has loaded.
if [ -f "$AGENT_UNIT" ]; then
  systemctl stop {{.AgentUnit}}
  trap 'systemctl start {{.AgentUnit}}' EXIT
fi
mv {{.Binary}}.new {{.Binary}}
# The agent state holds its private key (mode 0600); kafka only traverses it to read
# server.properties.
install -d -m 0711 -o brokerium-agent -g brokerium-agent "$STATE"
install -d -m 0755 -o brokerium-agent -g kafka "$KAFKA_DIR"
# Files created in the data directory belong to the kafka group, through which the agent formats
# and wipes it.
install -d -m 2770 -o kafka -g kafka "$DATA_DIR"
if [ ! -e "$LOG_DIR" ] && [ ! -h "$LOG_DIR" ]; then
  (umask 0007 && mkdir "$LOG_DIR")
  chown -h kafka:kafka "$LOG_DIR"
fi
if [ "$CONVERT" = 1 ]; then
  for f in node.json agent.pem agent.crt agent.key ca.crt state.json server.properties; do
    [ ! -e "$STATE/$f" ] || chown -h brokerium-agent:brokerium-agent "$STATE/$f"
  done
  # Should systemd restart Kafka before the agent takes over, it has to find the running release.
  RUNNING=
  [ ! -f "$KAFKA_UNIT" ] || RUNNING=$(sed -n 's|^ExecStart=\(/[^ ]*\)/bin/kafka-server-start\.sh .*|\1|p' "$KAFKA_UNIT")
  if [ -n "$RUNNING" ] && [ "$RUNNING" != "$KAFKA_DIR/current" ] && [ ! -e "$KAFKA_DIR/current" ] && [ ! -h "$KAFKA_DIR/current" ]; then
    ln -s "$RUNNING" "$KAFKA_DIR/current"
  fi
  chown -R brokerium-agent:kafka "$KAFKA_DIR"
fi
# Kafka created its files with umask 0022. The kafka account opens them to its group itself, so
# links placed in these directories cannot make root change other files.
runuser -u kafka -g kafka -- find "$DATA_DIR" "$LOG_DIR" -user kafka ! -type l \
  \( ! -group kafka -o ! -perm -g=rw \) -exec chgrp kafka {} + -exec chmod g+rwX {} + ||
  echo "some files in $DATA_DIR or $LOG_DIR stay closed to the kafka group; the agent may fail to wipe them" >&2
umask 077
{
  echo "BROKERIUM_AGENT_SERVER=${BROKERIUM_AGENT_SERVER}"
  echo "BROKERIUM_AGENT_TOKEN=${BROKERIUM_AGENT_TOKEN}"
  echo "BROKERIUM_AGENT_PROCESS=systemd"
  echo "BROKERIUM_AGENT_KAFKA_DATA_DIR=${DATA_DIR}"
  [ -z "${BROKERIUM_AGENT_KAFKA_CONFIG:-}" ] || echo "BROKERIUM_AGENT_KAFKA_CONFIG=${BROKERIUM_AGENT_KAFKA_CONFIG}"
  [ -z "${BROKERIUM_AGENT_PREINSTALLED:-}" ] || echo "BROKERIUM_AGENT_PREINSTALLED=${BROKERIUM_AGENT_PREINSTALLED}"
  [ -z "${BROKERIUM_AGENT_TRANSPORT:-}" ] || echo "BROKERIUM_AGENT_TRANSPORT=${BROKERIUM_AGENT_TRANSPORT}"
  [ -z "${BROKERIUM_AGENT_TLS:-}" ] || echo "BROKERIUM_AGENT_TLS=${BROKERIUM_AGENT_TLS}"
  [ -z "${BROKERIUM_AGENT_CA_FILE:-}" ] || echo "BROKERIUM_AGENT_CA_FILE=${BROKERIUM_AGENT_CA_FILE}"
  [ -z "$PROXY" ] || echo "HTTPS_PROXY=${PROXY}"
  [ -z "$PROXY" ] || [ -z "$NOPROXY" ] || echo "NO_PROXY=${NOPROXY}"
} > {{.EnvFile}}
umask 022
cat > "$KAFKA_UNIT" <<EOF
[Unit]
Description=Apache Kafka managed by brokerium-agent
After=network-online.target

[Service]
User=kafka
Group=kafka
UMask=0007
EnvironmentFile=-$STATE/kafka.env
Environment=LOG_DIR=$LOG_DIR
ExecStart=$KAFKA_DIR/current/bin/kafka-server-start.sh $KAFKA_CONFIG
KillSignal=SIGTERM
SuccessExitStatus=143
TimeoutStopSec=120
Restart=on-failure
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF
cat > "$AGENT_UNIT" <<EOF
[Unit]
Description=Brokerium agent for Apache Kafka
After=network-online.target
Wants=network-online.target

[Service]
User=brokerium-agent
Group=brokerium-agent
SupplementaryGroups=kafka
EnvironmentFile={{.EnvFile}}
ExecStart={{.Binary}} run
Restart=always
RestartSec=5
# The agent exits with {{.ExitNodeDeleted}} once its node was deleted in Brokerium.
RestartPreventExitStatus={{.ExitNodeDeleted}}
# sudo has to gain root for the Kafka unit: keep NoNewPrivileges off and add no option implying it.
NoNewPrivileges=no
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=$STATE $KAFKA_DIR $DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now {{.AgentUnit}}
trap - EXIT
echo "brokerium-agent installed; it runs as brokerium-agent and enrolls with the server on first start."
`

func (a *NodesAPI) handleInstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(installScript))
}
