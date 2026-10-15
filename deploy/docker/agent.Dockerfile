# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=none DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/bvaliev/brokerium/internal/version.Version=${VERSION} -X github.com/bvaliev/brokerium/internal/version.Commit=${COMMIT} -X github.com/bvaliev/brokerium/internal/version.Date=${DATE}" \
    -o /out/brokerium-agent ./cmd/brokerium-agent

# The agent image only carries the binary; on VMs the agent runs as a systemd service.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="brokerium-agent" \
      org.opencontainers.image.description="Brokerium node agent for Apache Kafka hosts" \
      org.opencontainers.image.source="https://github.com/bvaliev/brokerium" \
      org.opencontainers.image.licenses="LicenseRef-PolyForm-Shield-1.0.0"
COPY --from=build /out/brokerium-agent /usr/local/bin/brokerium-agent
COPY LICENSE NOTICE /licenses/
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/brokerium-agent"]
