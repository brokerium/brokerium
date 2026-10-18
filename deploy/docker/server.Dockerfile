# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

FROM --platform=$BUILDPLATFORM node:26.12.0-alpine AS web
WORKDIR /src/web
RUN npm install -g pnpm@12.11.2
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=none DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/brokerium/brokerium/internal/version.Version=${VERSION} -X github.com/brokerium/brokerium/internal/version.Commit=${COMMIT} -X github.com/brokerium/brokerium/internal/version.Date=${DATE}" \
    -o /out/brokerium-server ./cmd/brokerium-server && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="brokerium-server" \
      org.opencontainers.image.description="Brokerium control plane and dashboard for Apache Kafka clusters" \
      org.opencontainers.image.source="https://github.com/brokerium/brokerium" \
      org.opencontainers.image.licenses="LicenseRef-PolyForm-Shield-1.0.0"
COPY --from=build /out/brokerium-server /usr/local/bin/brokerium-server
COPY LICENSE NOTICE /licenses/
# Owned by the runtime user, so a named volume mounted here starts out writable.
COPY --from=build --chown=65532:65532 /out/data /var/lib/brokerium
ENV BROKERIUM_DATA_DIR=/var/lib/brokerium
WORKDIR /var/lib/brokerium
EXPOSE 8080 8443
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/brokerium-server"]
