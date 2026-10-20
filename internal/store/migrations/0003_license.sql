-- SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

-- The Brokerium Enterprise license key added through the dashboard. BROKERIUM_LICENSE and
-- BROKERIUM_LICENSE_FILE take precedence over it.
CREATE TABLE license (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    key        TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT ''
);
