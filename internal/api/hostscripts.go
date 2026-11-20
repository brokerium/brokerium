// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/brokerium/brokerium/internal/agenthost"
)

// scriptLayout fills the install and uninstall scripts with the layout of agent hosts. The
// install script writes the sudo rule to SudoersTemp first; SudoersLeftovers are such files of
// an interrupted install.
type scriptLayout struct {
	StateDir, KafkaDir, DataDir, EnvFile, Binary       string
	SudoersFile, SudoersTemp, SudoersLeftovers         string
	AgentUnit, KafkaUnit, AgentUnitFile, KafkaUnitFile string
	ExitNodeDeleted                                    int
}

// layoutUnder is the layout of agent hosts below root, "/" on hosts.
func layoutUnder(root string) scriptLayout {
	sudoers := filepath.Join(root, agenthost.SudoersFile)
	// sudo skips files whose names contain a dot.
	sibling := filepath.Join(filepath.Dir(sudoers), "."+filepath.Base(sudoers))
	return scriptLayout{
		StateDir:         filepath.Join(root, agenthost.StateDir),
		KafkaDir:         filepath.Join(root, agenthost.KafkaDir),
		DataDir:          filepath.Join(root, agenthost.DataDir),
		EnvFile:          filepath.Join(root, agenthost.EnvFile),
		Binary:           filepath.Join(root, agenthost.Binary),
		SudoersFile:      sudoers,
		SudoersTemp:      sibling + ".XXXXXX",
		SudoersLeftovers: sibling + ".*",
		AgentUnit:        agenthost.AgentUnit,
		KafkaUnit:        agenthost.KafkaUnit,
		AgentUnitFile:    filepath.Join(root, agenthost.UnitDir, agenthost.AgentUnit),
		KafkaUnitFile:    filepath.Join(root, agenthost.UnitDir, agenthost.KafkaUnit),
		ExitNodeDeleted:  agenthost.ExitNodeDeleted,
	}
}

var (
	installScript   = hostScript(installTemplate, layoutUnder("/"))
	uninstallScript = hostScript(uninstallTemplate, layoutUnder("/"))
)

func hostScript(text string, layout scriptLayout) string {
	var b strings.Builder
	if err := template.Must(template.New("").Parse(text)).Execute(&b, layout); err != nil {
		panic(err)
	}
	return b.String()
}

func (a *NodesAPI) handleUninstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(uninstallScript))
}

// uninstallTemplate removes brokerium-agent from a host that the install script set up.
const uninstallTemplate = `#!/bin/sh
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# Removes brokerium-agent from a host that the install script set up: {{.AgentUnit}},
# the agent binary, the sudo rule, {{.EnvFile}}, the agent state in
# {{.StateDir}} and the brokerium-agent account. Kafka runs from the configuration and
# logs in that state, so nothing changes while {{.KafkaUnit}} is active unless the script
# may stop Kafka; the unit of Kafka goes with the agent. Kafka releases and data stay unless
# requested:
#
#   --stop-kafka    stop Kafka when it runs
#   --remove-kafka  remove the Kafka releases in {{.KafkaDir}}
#   --delete-data   delete the Kafka data directory: BROKERIUM_AGENT_KAFKA_DATA_DIR of
#                   {{.EnvFile}}, default {{.DataDir}}; once that file is
#                   gone, pass BROKERIUM_AGENT_KAFKA_DATA_DIR after sudo
#
#   curl -fsSL <Brokerium URL>/api/v1/agent/uninstall.sh | sudo sh -s -- --stop-kafka
#
# Running it again removes what is left. The kafka account stays.
set -eu
usage() {
  echo "usage: uninstall.sh [--stop-kafka] [--remove-kafka] [--delete-data]"
}
STOP_KAFKA=0
REMOVE_KAFKA=0
DELETE_DATA=0
for arg in "$@"; do
  case "$arg" in
    --stop-kafka) STOP_KAFKA=1 ;;
    --remove-kafka) REMOVE_KAFKA=1 ;;
    --delete-data) DELETE_DATA=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option $arg" >&2; usage >&2; exit 2 ;;
  esac
done
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemctl not found: the agent runs as a systemd service" >&2; exit 1; }
STATE={{.StateDir}}
KAFKA_DIR={{.KafkaDir}}
ENV_FILE={{.EnvFile}}
AGENT_UNIT={{.AgentUnitFile}}
KAFKA_UNIT={{.KafkaUnitFile}}
if [ -f "$ENV_FILE" ]; then
  DATA_DIR=$(sed -n 's/^BROKERIUM_AGENT_KAFKA_DATA_DIR=//p' "$ENV_FILE" | tail -n 1)
  DATA_DIR="${DATA_DIR:-{{.DataDir}}}"
else
  DATA_DIR="${BROKERIUM_AGENT_KAFKA_DATA_DIR:-}"
fi
if [ "$DELETE_DATA" = 1 ]; then
  [ -n "$DATA_DIR" ] || {
    echo "the Kafka data directory is unknown because $ENV_FILE is gone: pass BROKERIUM_AGENT_KAFKA_DATA_DIR=<directory> after sudo" >&2
    exit 1
  }
  case "$DATA_DIR" in
    /*[!/]*) ;;
    *) echo "refusing to delete $DATA_DIR: not the absolute path of a directory" >&2; exit 1 ;;
  esac
  case "$DATA_DIR" in
    *[!A-Za-z0-9._/@:+-]* | */. | */./* | */.. | */../*)
      echo "refusing to delete $DATA_DIR: use a path of letters, digits and ._/@:+- without . or .. parts" >&2; exit 1 ;;
  esac
fi
KAFKA_ACTIVE=0
if systemctl is-active --quiet {{.KafkaUnit}}; then KAFKA_ACTIVE=1; fi
if [ "$KAFKA_ACTIVE" = 1 ] && [ "$STOP_KAFKA" = 0 ]; then
  echo "Kafka runs on this host as {{.KafkaUnit}}, from the configuration of the agent; nothing was changed." >&2
  echo "If its node still belongs to a cluster, decommission it in Brokerium first. Then run this again with --stop-kafka." >&2
  exit 1
fi

# The agent goes first, so that it does not start Kafka again.
if [ -f "$AGENT_UNIT" ] || systemctl is-active --quiet {{.AgentUnit}}; then
  systemctl disable --now {{.AgentUnit}} >/dev/null 2>&1 || systemctl stop {{.AgentUnit}}
  rm -f "$AGENT_UNIT"
  echo "stopped {{.AgentUnit}} and removed $AGENT_UNIT"
fi
if [ -f "$KAFKA_UNIT" ] || [ "$KAFKA_ACTIVE" = 1 ]; then
  [ "$KAFKA_ACTIVE" = 0 ] || echo "stopping Kafka; a controlled shutdown takes up to two minutes"
  systemctl disable --now {{.KafkaUnit}} >/dev/null 2>&1 || systemctl stop {{.KafkaUnit}}
  rm -f "$KAFKA_UNIT"
  echo "stopped {{.KafkaUnit}} and removed $KAFKA_UNIT"
fi
systemctl daemon-reload
systemctl reset-failed {{.AgentUnit}} {{.KafkaUnit}} >/dev/null 2>&1 || true
rm -f {{.SudoersLeftovers}} {{.Binary}}.new
for f in {{.SudoersFile}} "$ENV_FILE" {{.Binary}}; do
  if [ -e "$f" ]; then
    rm -f "$f"
    echo "removed $f"
  fi
done
if [ -e "$STATE" ]; then
  rm -rf "$STATE"
  echo "removed $STATE: the identity of the agent, the configuration and logs of Kafka"
fi
if id brokerium-agent >/dev/null 2>&1; then
  userdel brokerium-agent
  echo "removed the account brokerium-agent"
fi
if getent group brokerium-agent >/dev/null; then groupdel brokerium-agent; fi
if [ -e "$KAFKA_DIR" ]; then
  if [ "$REMOVE_KAFKA" = 1 ]; then
    rm -rf "$KAFKA_DIR"
    rmdir "$(dirname "$KAFKA_DIR")" 2>/dev/null || true
    echo "removed the Kafka releases in $KAFKA_DIR"
  else
    echo "kept the Kafka releases in $KAFKA_DIR; --remove-kafka removes them"
  fi
fi
if [ -n "$DATA_DIR" ] && [ -e "$DATA_DIR" ]; then
  if [ "$DELETE_DATA" = 1 ]; then
    rm -rf "$DATA_DIR"
    echo "deleted the Kafka data in $DATA_DIR"
  else
    echo "kept the Kafka data in $DATA_DIR; --delete-data deletes it"
  fi
fi
echo "brokerium-agent is removed from this host."
`
