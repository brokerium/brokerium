-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- The state of the findings of the problem rules (ADR 0023). The findings are evaluated again
-- after every snapshot and not stored; a state remembers when a finding was first and last seen,
-- who acknowledged it and until when it is muted. fingerprint identifies the code and the object
-- of a finding; object is "<kind>:<id>", for example "topic:orders" or "broker:101".
CREATE TABLE problem_states (
    cluster_id      TEXT NOT NULL REFERENCES clusters (id) ON DELETE CASCADE,
    fingerprint     TEXT NOT NULL,
    code            TEXT NOT NULL,
    object          TEXT NOT NULL,
    first_seen      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen       TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_by TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ,
    muted_until     TIMESTAMPTZ,
    PRIMARY KEY (cluster_id, fingerprint)
);

CREATE INDEX problem_states_by_last_seen ON problem_states (last_seen);
