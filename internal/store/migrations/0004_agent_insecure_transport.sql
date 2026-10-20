-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- Agents behind TLS-terminating proxies enroll with the insecure transport (ADR 0017): they
-- authenticate their messages with a node secret, sealed with the server key in
-- secret_ciphertext, instead of a client certificate.
ALTER TABLE nodes
    ADD COLUMN transport         TEXT NOT NULL DEFAULT 'mtls',
    ADD COLUMN secret_ciphertext BYTEA;
