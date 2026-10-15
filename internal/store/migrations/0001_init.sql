-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

CREATE TABLE clusters (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    provider          TEXT NOT NULL,
    flavor            TEXT NOT NULL DEFAULT 'apache',
    bootstrap_servers TEXT[] NOT NULL,
    security          JSONB NOT NULL DEFAULT '{}',
    secrets           BYTEA,
    labels            JSONB NOT NULL DEFAULT '{}',
    settings          JSONB NOT NULL DEFAULT '{}',
    kafka_cluster_id  TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL,
    disabled      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE operations (
    id               TEXT PRIMARY KEY,
    cluster_id       TEXT NOT NULL REFERENCES clusters (id) ON DELETE CASCADE,
    type             TEXT NOT NULL,
    status           TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',
    params           JSONB NOT NULL DEFAULT '{}',
    plan             JSONB NOT NULL DEFAULT '{}',
    irreversible     BOOLEAN NOT NULL DEFAULT false,
    current_step     INT NOT NULL DEFAULT 0,
    requested_action TEXT NOT NULL DEFAULT '',
    error            TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX operations_one_active_per_cluster
    ON operations (cluster_id) WHERE status IN ('pending', 'running', 'paused');
CREATE INDEX operations_by_cluster ON operations (cluster_id, created_at DESC);

CREATE TABLE operation_steps (
    operation_id TEXT NOT NULL REFERENCES operations (id) ON DELETE CASCADE,
    idx          INT NOT NULL,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    args         JSONB NOT NULL DEFAULT '{}',
    always_run   BOOLEAN NOT NULL DEFAULT false,
    status       TEXT NOT NULL DEFAULT 'pending',
    detail       JSONB NOT NULL DEFAULT '{}',
    error        TEXT NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    PRIMARY KEY (operation_id, idx)
);

CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    cluster_id TEXT NOT NULL DEFAULT '',
    target     TEXT NOT NULL DEFAULT '',
    result     TEXT NOT NULL DEFAULT 'ok',
    detail     JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX audit_log_by_time ON audit_log (at DESC);

CREATE TABLE nodes (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    cluster_id       TEXT REFERENCES clusters (id) ON DELETE SET NULL,
    role             TEXT NOT NULL DEFAULT '',
    kafka_node_id    INT,
    hostname         TEXT NOT NULL DEFAULT '',
    agent_version    TEXT NOT NULL DEFAULT '',
    cert_fingerprint TEXT NOT NULL DEFAULT '',
    status           JSONB NOT NULL DEFAULT '{}',
    labels           JSONB NOT NULL DEFAULT '{}',
    provider_ref     TEXT NOT NULL DEFAULT '',
    last_seen_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE join_tokens (
    token_hash TEXT PRIMARY KEY,
    node_name  TEXT NOT NULL,
    cluster_id TEXT REFERENCES clusters (id) ON DELETE CASCADE,
    created_by TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE certificate_authority (
    id             INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    cert_pem       TEXT NOT NULL,
    key_ciphertext BYTEA NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
