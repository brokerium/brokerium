-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- Settings of the installation that belong to no cluster, such as the Prometheus data source of
-- the monitoring feature (ADR 0024). value holds the plain settings as JSON, secrets the
-- credentials sealed with the secret key of the server.
CREATE TABLE installation_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL DEFAULT '{}',
    secrets    BYTEA,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT ''
);

-- How the metrics of a cluster are found in Prometheus: a PromQL label selector that replaces
-- the default one, and the port of the exporter on the brokers for the default selector (0 means
-- the default port).
CREATE TABLE cluster_monitoring (
    cluster_id    TEXT PRIMARY KEY REFERENCES clusters (id) ON DELETE CASCADE,
    selector      TEXT NOT NULL DEFAULT '',
    exporter_port INT NOT NULL DEFAULT 0 CHECK (exporter_port BETWEEN 0 AND 65535),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT NOT NULL DEFAULT ''
);
