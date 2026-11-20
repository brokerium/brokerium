// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	// baseImage runs the release tarballs of releases without an apache/kafka image.
	baseImage = "eclipse-temurin:17-jre-noble"
	clusterID = "YnJva2VyaXVtLWNhdGFsbw"
	topicName = "brokerium-catalog"
	// readyTimeout is how long a node may take until it creates the topic.
	readyTimeout = 3 * time.Minute
)

// nodeConfig is the whole configuration of the node: the keys a combined KRaft node needs, and
// nothing a topic inherits, so that the topic reports the defaults of Kafka. {port} is the port of
// the client listener, the same inside the container and on the host.
var nodeConfig = []string{
	"process.roles=broker,controller",
	"node.id=1",
	"controller.quorum.voters=1@localhost:9093",
	"controller.listener.names=CONTROLLER",
	"listeners=PLAINTEXT://:{port},CONTROLLER://:9093",
	"advertised.listeners=PLAINTEXT://127.0.0.1:{port}",
	"listener.security.protocol.map=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
	"log.dirs=/tmp/kraft-combined-logs",
}

// startScript writes the configuration, formats the storage and starts Kafka. KAFKA_TGZ names a
// release tarball to unpack first.
const startScript = `set -eu
home=/opt/kafka
if [ -n "${KAFKA_TGZ:-}" ]; then
  mkdir -p "$home"
  tar -xzf "$KAFKA_TGZ" --strip-components=1 -C "$home"
fi
printf '%s\n' "$NODE_CONFIG" > /tmp/server.properties
"$home/bin/kafka-storage.sh" format --cluster-id "$CLUSTER_ID" --config /tmp/server.properties
exec "$home/bin/kafka-server-start.sh" /tmp/server.properties
`

// node is a running single-node Kafka cluster.
type node struct {
	container string
	addr      string
}

// startNode starts a container with the release and waits until it has created the topic.
func startNode(ctx context.Context, v version, cache string) (*node, error) {
	image := "apache/kafka:" + v.String()
	var extra []string
	if !v.hasImage() {
		tgz, err := tarball(ctx, cache, v)
		if err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(tgz)
		if err != nil {
			return nil, err
		}
		image = baseImage
		extra = []string{"-v", abs + ":/dist/kafka.tgz:ro", "-e", "KAFKA_TGZ=/dist/kafka.tgz"}
	}
	if err := ensureImage(ctx, image); err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	n := &node{container: "brokerium-catalog-" + v.String(), addr: "127.0.0.1:" + strconv.Itoa(port)}
	// A container left behind by a run that was killed.
	n.remove()
	config := strings.ReplaceAll(strings.Join(nodeConfig, "\n"), "{port}", strconv.Itoa(port))
	args := append([]string{"run", "--detach", "--name", n.container, "--memory", "1g",
		"--publish", fmt.Sprintf("127.0.0.1:%d:%d", port, port),
		"--env", "KAFKA_HEAP_OPTS=-Xms384m -Xmx384m", "--env", "LOG_DIR=/tmp/kafka-server-logs",
		"--env", "CLUSTER_ID=" + clusterID, "--env", "NODE_CONFIG=" + config}, extra...)
	args = append(args, "--entrypoint", "bash", image, "-c", startScript)
	slog.Info("starting Kafka", "version", v, "image", image, "container", n.container, "addr", n.addr)
	_, err = docker(ctx, args...)
	if err == nil {
		err = n.createTopic(ctx)
	}
	if err != nil {
		n.remove()
		return nil, err
	}
	return n, nil
}

// remove deletes the container, also after the context of the release ended.
func (n *node) remove() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := docker(ctx, "rm", "--force", "--volumes", n.container); err != nil {
		slog.Warn("removing the container", "container", n.container, "err", err)
	}
}

func (n *node) client() (*kgo.Client, error) {
	return kgo.NewClient(kgo.SeedBrokers(n.addr), kgo.ClientID("brokerium-catalog"),
		kgo.DialTimeout(5*time.Second), kgo.RequestTimeoutOverhead(10*time.Second), kgo.RetryTimeout(10*time.Second))
}

// createTopic creates the topic with the defaults of Kafka, retrying while the node starts.
func (n *node) createTopic(ctx context.Context) error {
	cl, err := n.client()
	if err != nil {
		return err
	}
	defer cl.Close()
	ready, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	adm := kadm.NewClient(cl)
	for {
		attempt, cancelAttempt := context.WithTimeout(ready, 10*time.Second)
		_, err := adm.CreateTopic(attempt, 1, 1, nil, topicName)
		cancelAttempt()
		switch {
		case err == nil || errors.Is(err, kerr.TopicAlreadyExists):
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case !n.running():
			return fmt.Errorf("kafka stopped while starting:\n%s", n.logs())
		}
		select {
		case <-ready.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("kafka did not create the topic within %s (%w):\n%s", readyTimeout, err, n.logs())
		case <-time.After(time.Second):
		}
	}
}

// describe reads the configuration of the broker and of the topic with synonyms and
// documentation.
func (n *node) describe(ctx context.Context) (broker, topic []kmsg.DescribeConfigsResponseResourceConfig, err error) {
	cl, err := n.client()
	if err != nil {
		return nil, nil, err
	}
	defer cl.Close()
	if broker, err = describeConfigs(ctx, cl, kmsg.ConfigResourceTypeBroker, "1"); err != nil {
		return nil, nil, fmt.Errorf("broker configuration: %w", err)
	}
	// The topic may take a moment to reach the metadata of the broker.
	for attempt := 0; ; attempt++ {
		topic, err = describeConfigs(ctx, cl, kmsg.ConfigResourceTypeTopic, topicName)
		if err == nil || !errors.Is(err, kerr.UnknownTopicOrPartition) || attempt == 10 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("topic configuration: %w", err)
	}
	return broker, topic, nil
}

func describeConfigs(ctx context.Context, cl *kgo.Client, typ kmsg.ConfigResourceType, name string) ([]kmsg.DescribeConfigsResponseResourceConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req := kmsg.NewPtrDescribeConfigsRequest()
	req.IncludeSynonyms = true
	req.IncludeDocumentation = true
	res := kmsg.NewDescribeConfigsRequestResource()
	res.ResourceType = typ
	res.ResourceName = name
	req.Resources = append(req.Resources, res)
	resp, err := req.RequestWith(ctx, cl)
	if err != nil {
		return nil, err
	}
	// Documentation and types arrived with version 3.
	if resp.Version < 3 {
		return nil, fmt.Errorf("the broker answers DescribeConfigs version %d, without documentation", resp.Version)
	}
	if len(resp.Resources) != 1 {
		return nil, fmt.Errorf("%d resources in the response", len(resp.Resources))
	}
	r := resp.Resources[0]
	if err := kerr.ErrorForCode(r.ErrorCode); err != nil {
		if r.ErrorMessage != nil {
			return nil, fmt.Errorf("%w: %s", err, *r.ErrorMessage)
		}
		return nil, err
	}
	return r.Configs, nil
}

func (n *node) running() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := docker(ctx, "inspect", "--format", "{{.State.Running}}", n.container)
	return err == nil && strings.TrimSpace(out) == "true"
}

func (n *node) logs() string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := docker(ctx, "logs", "--tail", "40", n.container)
	if err != nil {
		return err.Error()
	}
	return out
}

// ensureImage pulls an image that is not present.
func ensureImage(ctx context.Context, image string) error {
	if _, err := docker(ctx, "image", "inspect", "--format", "{{.Id}}", image); err == nil {
		return nil
	}
	slog.Info("pulling the image", "image", image)
	_, err := docker(ctx, "pull", "--quiet", image)
	return err
}

// docker runs a Docker command and returns its output: stdout and stderr combined.
func docker(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// freePort is a TCP port that nothing listens on right now.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("finding a free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}
