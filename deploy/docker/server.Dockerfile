# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

# community, or enterprise for the image of Brokerium Enterprise (ADR 0016).
ARG EDITION=community

FROM --platform=$BUILDPLATFORM node:24.22.0-alpine AS web
ARG EDITION
WORKDIR /src/web
RUN npm install -g pnpm@12.11.2
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN VITE_EDITION=$EDITION pnpm build && node scripts/check-edition.mjs "$EDITION" dist

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT=none DATE=unknown EDITION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ internal/webui/dist/
RUN tags=""; if [ "$EDITION" = enterprise ]; then tags=enterprise; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -tags "$tags" \
    -ldflags "-s -w -X github.com/brokerium/brokerium/internal/version.Version=${VERSION} -X github.com/brokerium/brokerium/internal/version.Commit=${COMMIT} -X github.com/brokerium/brokerium/internal/version.Date=${DATE}" \
    -o /out/brokerium-server ./cmd/brokerium-server && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot AS runtime-community
LABEL org.opencontainers.image.title="brokerium-server" \
      org.opencontainers.image.description="Brokerium control plane and dashboard for Apache Kafka clusters" \
      org.opencontainers.image.licenses="LicenseRef-PolyForm-Shield-1.0.0"

FROM gcr.io/distroless/static-debian12:nonroot AS runtime-enterprise
LABEL org.opencontainers.image.title="brokerium-server-enterprise" \
      org.opencontainers.image.description="Brokerium Enterprise control plane and dashboard for Apache Kafka clusters" \
      org.opencontainers.image.licenses="LicenseRef-PolyForm-Shield-1.0.0 AND LicenseRef-Brokerium-Commercial"
COPY ee/LICENSE /licenses/LICENSE-enterprise

FROM runtime-${EDITION}
LABEL org.opencontainers.image.source="https://github.com/brokerium/brokerium"
COPY --from=build /out/brokerium-server /usr/local/bin/brokerium-server
COPY LICENSE NOTICE /licenses/
# Owned by the runtime user, so a named volume mounted here starts out writable.
COPY --from=build --chown=65532:65532 /out/data /var/lib/brokerium
ENV BROKERIUM_DATA_DIR=/var/lib/brokerium
WORKDIR /var/lib/brokerium
EXPOSE 8080 8443
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/brokerium-server"]
