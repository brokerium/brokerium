# ADR 0019: Docker clusters for Docker installations

- Status: accepted
- Date: 2026-10-20

## Context

The Docker provider creates Kafka clusters from the lab node image as containers ("Create a
cluster in Docker"). The dashboard offered it on every server, although it worked only for a
server running directly on the Docker host: nodes publish their listeners on ports of the host,
and Brokerium recorded `localhost:<port>` as their addresses. A server in a container, in
Kubernetes or on another machine created containers it could not reach, or failed later for lack
of a Docker socket.

A Docker socket alone is not the right signal either: Kubernetes nodes often have one, and Kafka
must not be started in Docker next to the workloads of a Kubernetes cluster. Docker clusters are
meant for evaluations and labs with the Docker installation of Brokerium (`docker run`, see the
installation guide) and for development.

## Decision

- `BROKERIUM_DOCKER_PROVIDER=auto|on|off` (default `auto`) decides whether the server offers
  Docker clusters.
  - `auto` offers them only in the Docker installation: the server runs in a Docker container
    (`/.dockerenv` exists), not in Kubernetes (`KUBERNETES_SERVICE_HOST` is not set), and the
    Docker API at `BROKERIUM_DOCKER_HOST` answers a ping within two seconds. The result of the
    ping is reused for 30 seconds, so a Docker daemon that starts or stops later is noticed
    without a ping on every request.
  - `on` offers them regardless, for development with the server on the Docker host (`make dev`,
    the end-to-end tests).
  - `off` never offers them. The Helm chart sets it.
- `GET /api/v1/server` reports the version, the edition and `providers.docker` with `available`
  and a `reason` that names the condition that failed, including the error of the ping, such as
  permission denied on the socket. The dashboard offers Docker clusters only when they are
  available.
- Without Docker clusters, creating a Docker cluster fails with 409 `docker_unavailable` and the
  reason, and so do planning, confirming and resuming an operation whose steps act on the nodes of
  a Docker cluster: the operations engine asks the node manager before it stores a plan and before
  it starts or resumes one. Existing Docker clusters stay registered and monitored over the Kafka
  protocol, and Kafka-level operations on them keep working.
- Network mode: a server in a container reaches the nodes of Docker clusters by container name on
  `BROKERIUM_LAB_NETWORK`, brokers at `<container>:9092` (their internal listener) and
  controllers at `<container>:9093`, and records these as the bootstrap and controller servers of
  the cluster. A server on the Docker host keeps using the ports published on `localhost`. In both
  modes every broker publishes a listener on `localhost:<port base + node id>` for clients on the
  Docker host.
- The agents of the nodes reach the server at `BROKERIUM_AGENT_ENDPOINT`, which the Docker
  installation sets to the server container on the lab network, `brokerium:8443`. The host of the
  endpoint is always a name of the agent gateway certificate, so the agents can verify it.
- The provider pulls a missing node image before it creates a container, so the Docker
  installation needs no manual `docker pull`.

## Consequences

- The Docker installation runs the server on the network `brokerium-lab` with the Docker socket
  mounted and the group of the socket added with `--group-add`, because the server image runs as
  a non-root user. Whoever may create clusters in Brokerium can then start containers on the
  Docker host, so this installation stays one for evaluations and labs.
- Servers that run on the Docker host and create Docker clusters, in development and in the
  end-to-end tests, need `BROKERIUM_DOCKER_PROVIDER=on`; `make dev` sets it.
- Clusters keep the addresses recorded when they were created. After a server moves between the
  Docker host and a container, the bootstrap servers of existing Docker clusters have to be
  changed to the addresses of the new mode.
- Container runtimes that do not create `/.dockerenv` need `on` to offer Docker clusters.
