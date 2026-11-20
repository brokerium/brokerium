# syntax=docker/dockerfile:1.7
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Apache Kafka from its release tarball, for lab clusters on releases that have no apache/kafka
# image (those start at 3.7.0). KAFKA_SHA512 is the checksum Apache publishes next to the tarball
# (kafka_<scala>-<version>.tgz.sha512 on archive.apache.org). A tarball in dist/ of the build
# context is used instead of a download, for example one cached by CI.

ARG BASE_IMAGE=eclipse-temurin:17-jre-noble

FROM ${BASE_IMAGE} AS download
ARG KAFKA_VERSION
ARG KAFKA_SHA512
ARG SCALA_VERSION=2.13
WORKDIR /tmp
RUN --mount=type=bind,target=/context set -eu; \
    : "${KAFKA_VERSION:?}" "${KAFKA_SHA512:?}"; \
    file="kafka_${SCALA_VERSION}-${KAFKA_VERSION}.tgz"; \
    if [ -f "/context/dist/$file" ]; then \
      cp "/context/dist/$file" .; \
    else \
      for url in "https://dlcdn.apache.org/kafka/${KAFKA_VERSION}/${file}" \
                 "https://archive.apache.org/dist/kafka/${KAFKA_VERSION}/${file}"; do \
        if curl -fsSL --connect-timeout 20 --speed-limit 10240 --speed-time 60 -o "$file" "$url"; then break; fi; \
        rm -f "$file"; \
      done; \
    fi; \
    echo "${KAFKA_SHA512}  ${file}" | sha512sum -c -; \
    mkdir /opt/kafka; \
    tar -xzf "$file" --strip-components=1 -C /opt/kafka; \
    rm -rf /opt/kafka/site-docs /opt/kafka/bin/windows

FROM ${BASE_IMAGE}
ARG KAFKA_VERSION
LABEL org.opencontainers.image.title="Apache Kafka ${KAFKA_VERSION} (Brokerium lab)" \
      org.opencontainers.image.source="https://github.com/brokerium/brokerium"
RUN useradd --system --uid 1001 --home-dir /var/lib/kafka --shell /usr/sbin/nologin kafka && \
    mkdir -p /var/lib/kafka/data /var/lib/kafka/logs && chown -R kafka:kafka /var/lib/kafka
COPY --from=download /opt/kafka /opt/kafka
ENV PATH=/opt/kafka/bin:$PATH \
    LOG_DIR=/var/lib/kafka/logs
USER kafka
WORKDIR /var/lib/kafka
CMD ["kafka-server-start.sh", "/opt/kafka/config/server.properties"]
