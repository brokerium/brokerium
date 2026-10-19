-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- Agents renew their certificates. cert_not_after is the expiry of the current certificate;
-- prev_cert_fingerprint is the certificate a renewal was requested with, accepted until the
-- agent connects with the new one.
ALTER TABLE nodes
    ADD COLUMN cert_not_after        TIMESTAMPTZ,
    ADD COLUMN prev_cert_fingerprint TEXT NOT NULL DEFAULT '';
