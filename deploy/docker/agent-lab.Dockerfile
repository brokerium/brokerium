# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# syntax=docker/dockerfile:1.7
#
# Lab node image: Apache Kafka plus brokerium-agent as the main process. The Docker provider of
# brokerium-server starts these containers to simulate virtual machines (ADR 0004).

ARG KAFKA_IMAGE=apache/kafka:4.3.1

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/bvaliev/brokerium/internal/version.Version=${VERSION}" \
    -o /out/brokerium-agent ./cmd/brokerium-agent

FROM ${KAFKA_IMAGE}
LABEL org.opencontainers.image.title="brokerium-agent (lab node)" \
      org.opencontainers.image.source="https://github.com/bvaliev/brokerium"
USER root
RUN mkdir -p /var/lib/brokerium-agent /opt/brokerium/kafka && chown -R appuser:appuser /var/lib/brokerium-agent /opt/brokerium /var/lib/kafka
COPY --from=build /out/brokerium-agent /usr/local/bin/brokerium-agent
USER appuser
ENV BROKERIUM_AGENT_DATA_DIR=/var/lib/brokerium-agent \
    BROKERIUM_AGENT_KAFKA_DIR=/opt/brokerium/kafka \
    BROKERIUM_AGENT_KAFKA_DATA_DIR=/var/lib/kafka/data \
    BROKERIUM_AGENT_PREINSTALLED=/opt/kafka
ENTRYPOINT ["/usr/local/bin/brokerium-agent", "run"]
