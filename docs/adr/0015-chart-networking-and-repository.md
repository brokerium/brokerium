# ADR 0015: Helm chart networking and chart repository

- Status: accepted
- Date: 2026-10-19

## Context

The server has two ports: the dashboard and API over plain HTTP, and the agent gateway, where
agents authenticate with mutual TLS against the server's own CA and pin that CA. A proxy may
therefore terminate TLS for the dashboard but must pass the agent port through as TCP or TLS.
The 0.1 chart offered an Ingress, which only handles HTTP, and a switch of the service type. It
was installed from a checkout of the repository.

## Decision

- **Networking.** The chart drops the Ingress and always creates a ClusterIP service with both
  ports. Two optional ways in:
  - a `LoadBalancer` service with the selected ports in one service (one address and one DNS
    record through the `external-dns.alpha.kubernetes.io/hostname` annotation), which every
    major load balancer supports;
  - Gateway API (`gateway.networking.k8s.io/v1`, Gateway API 1.5 or later): a created Gateway
    or `parentRefs` of an existing one, an `HTTPRoute` for the dashboard with HTTPS at the
    gateway, and a `TLSRoute` in passthrough mode for agents, routed by SNI. A `GRPCRoute` is
    not used, because the gateway would terminate TLS and break mutual TLS.
- The chart derives the public URL, the agent endpoint and the names in the gateway
  certificate from these settings. `values.schema.json` rejects unknown and removed values.
- **Repository.** The release workflow attaches the packaged chart to the GitHub release,
  pushes it to `oci://ghcr.io/brokerium/charts` and rebuilds the Helm repository
  https://charts.brokerium.kaufen: GitHub Pages of the product repository, deployed from an
  Actions artifact whose `index.yaml` points at the chart of every release. There is no
  `gh-pages` branch, and the index can be rebuilt from the releases at any time.
- The `helm` workflow lints the chart, validates the renders of every networking mode against
  the Kubernetes and Gateway API schemas and a kind API server, and installs it on kind.

## Consequences

- Upgrading from chart 0.1 needs new values; the schema reports the removed ones.
- The plain HTTP port of a LoadBalancer belongs on internal networks or behind provider TLS.
- The Pages site needs the `charts` DNS record without proxying, the custom domain in the
  repository settings and a deployment rule for `v*` tags in the `github-pages` environment.
- Artifact Hub lists the repository once it is registered there; its repository id goes into
  the `ARTIFACTHUB_REPOSITORY_ID` variable.
