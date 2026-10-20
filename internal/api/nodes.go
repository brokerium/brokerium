// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/agentgw"
	"github.com/brokerium/brokerium/internal/provider/strimzi"
	"github.com/brokerium/brokerium/internal/store"
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
	// Proxy and NoProxy go into the install command as HTTPS_PROXY and NO_PROXY.
	Proxy, NoProxy string
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
	s.Route("DELETE /api/v1/nodes/{id}", store.RoleOperator, a.handleDelete)
	s.mux.HandleFunc("GET /api/v1/agent/install.sh", a.handleInstallScript)
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
		out = append(out, NodeView{Node: n, Connected: a.Gateway.Connected(n.ID),
			CertificateState: certificateState(n, time.Now(), a.CertificateWarning)})
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
	WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "expiresAt": expires, "command": a.installCommand(token)})
}

// installCommand is the command that installs the agent on a host. sudo drops the proxy
// variables of the caller, so they are passed to curl and to the script explicitly.
func (a *NodesAPI) installCommand(token string) string {
	proxy := ""
	if a.Proxy != "" {
		proxy = "HTTPS_PROXY=" + shellQuote(a.Proxy) + " "
		if a.NoProxy != "" {
			proxy += "NO_PROXY=" + shellQuote(a.NoProxy) + " "
		}
	}
	return fmt.Sprintf("%scurl -fsSL %s/api/v1/agent/install.sh | sudo %sBROKERIUM_AGENT_SERVER=%s BROKERIUM_AGENT_TOKEN=%s sh",
		proxy, strings.TrimRight(a.PublicURL, "/"), proxy, a.AgentEndpoint, token)
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
# Installs brokerium-agent as a systemd service under its own account, brokerium-agent. Kafka runs
# as its own unit (brokerium-kafka.service) under the kafka account, so restarting the agent never
# stops Kafka. The agent reaches the Kafka directories through the kafka group, and a sudo rule
# lets it start, stop and restart that unit and nothing else. Running the script again on a host
# whose agent runs as root moves the agent to its own account; a running Kafka keeps running.
#
#   BROKERIUM_AGENT_SERVER, BROKERIUM_AGENT_TOKEN  required (Nodes page of the dashboard)
#   BROKERIUM_AGENT_VERSION                        release tag, default: latest
#   BROKERIUM_AGENT_URL                            download URL of the binary (air-gapped installs)
#   BROKERIUM_AGENT_SHA256                         expected SHA-256 of that binary
#   BROKERIUM_AGENT_KAFKA_DATA_DIR                 log.dirs of new nodes, default /var/lib/kafka/data
#   BROKERIUM_AGENT_KAFKA_CONFIG                   adopt a running node: its server.properties
#   BROKERIUM_AGENT_PREINSTALLED                   and its Kafka installation directory
#   HTTPS_PROXY, NO_PROXY                          HTTP CONNECT proxy for the downloads and for the
#                                                  agent; pass them after sudo, which drops them
set -eu
: "${BROKERIUM_AGENT_SERVER:?set BROKERIUM_AGENT_SERVER}"
: "${BROKERIUM_AGENT_TOKEN:?set BROKERIUM_AGENT_TOKEN}"
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
SYSTEMCTL=$(command -v systemctl) || { echo "systemctl not found: the agent runs as a systemd service" >&2; exit 1; }
if ! command -v sudo >/dev/null || ! command -v visudo >/dev/null || [ ! -d /etc/sudoers.d ]; then
  echo "sudo is not installed: install it (with /etc/sudoers.d) and run this again; the agent starts and stops Kafka through a sudo rule" >&2
  exit 1
fi
VERSION="${BROKERIUM_AGENT_VERSION:-latest}"
STATE=/var/lib/brokerium-agent
KAFKA_DIR=/opt/brokerium/kafka
LOG_DIR="$STATE/logs"
DATA_DIR="${BROKERIUM_AGENT_KAFKA_DATA_DIR:-/var/lib/kafka/data}"
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
AGENT_UNIT=/etc/systemd/system/brokerium-agent.service
KAFKA_UNIT=/etc/systemd/system/brokerium-kafka.service
# On a host whose agent ran as root, the files of the agent move to its account below.
CONVERT=1
if grep -qx 'User=brokerium-agent' "$AGENT_UNIT" 2>/dev/null; then CONVERT=0; fi
getent group kafka >/dev/null || groupadd --system kafka
id kafka >/dev/null 2>&1 || useradd --system --gid kafka --home-dir /var/lib/kafka --shell /usr/sbin/nologin kafka
getent group brokerium-agent >/dev/null || groupadd --system brokerium-agent
id brokerium-agent >/dev/null 2>&1 ||
  useradd --system --gid brokerium-agent --home-dir "$STATE" --shell /usr/sbin/nologin brokerium-agent
usermod -a -G kafka brokerium-agent
# sudo skips files whose names contain a dot, so the rule takes effect only once visudo accepted it.
RULE=$(mktemp /etc/sudoers.d/.brokerium-agent.XXXXXX)
cat > "$RULE" <<EOF
Defaults:brokerium-agent !requiretty
brokerium-agent ALL=(root) NOPASSWD: $SYSTEMCTL start brokerium-kafka.service, $SYSTEMCTL stop brokerium-kafka.service, $SYSTEMCTL restart brokerium-kafka.service
EOF
chmod 0440 "$RULE"
if ! visudo -cf "$RULE" >/dev/null; then
  rm -f "$RULE"
  echo "visudo rejected the sudo rule for brokerium-agent; the agent was not installed or changed" >&2
  exit 1
fi
mv -f "$RULE" /etc/sudoers.d/brokerium-agent
if ! sudo -n -l -U brokerium-agent "$SYSTEMCTL" restart brokerium-kafka.service >/dev/null 2>&1; then
  echo "sudo ignores /etc/sudoers.d/brokerium-agent (does /etc/sudoers include /etc/sudoers.d?); the agent was not installed or changed" >&2
  exit 1
fi
curl -fsSL "$URL" -o /usr/local/bin/brokerium-agent.new
if [ -n "${BROKERIUM_AGENT_URL:-}" ]; then
  WANT="${BROKERIUM_AGENT_SHA256:-}"
else
  # Release binaries are checked against the SHA256SUMS of the same release.
  WANT=$(curl -fsSL "${URL%/*}/SHA256SUMS" | awk -v f="brokerium-agent-linux-${ARCH}" '$2 == f {print $1}')
  [ -n "$WANT" ] || { echo "no checksum for brokerium-agent-linux-${ARCH} in the release" >&2; exit 1; }
fi
if [ -n "$WANT" ] && [ "$(sha256sum /usr/local/bin/brokerium-agent.new | cut -d' ' -f1)" != "$WANT" ]; then
  rm -f /usr/local/bin/brokerium-agent.new
  echo "checksum mismatch for $URL" >&2
  exit 1
fi
chmod 0755 /usr/local/bin/brokerium-agent.new
# Only the agent restarts. If a later step fails, it starts again with the unit systemd has loaded.
if [ -f "$AGENT_UNIT" ]; then
  systemctl stop brokerium-agent.service
  trap 'systemctl start brokerium-agent.service' EXIT
fi
mv /usr/local/bin/brokerium-agent.new /usr/local/bin/brokerium-agent
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
  [ -z "$PROXY" ] || echo "HTTPS_PROXY=${PROXY}"
  [ -z "$PROXY" ] || [ -z "$NOPROXY" ] || echo "NO_PROXY=${NOPROXY}"
} > /etc/brokerium-agent.env
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
EnvironmentFile=/etc/brokerium-agent.env
ExecStart=/usr/local/bin/brokerium-agent run
Restart=always
RestartSec=5
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
systemctl enable --now brokerium-agent.service
trap - EXIT
echo "brokerium-agent installed; it runs as brokerium-agent and enrolls with the server on first start."
`

func (a *NodesAPI) handleInstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(installScript))
}
