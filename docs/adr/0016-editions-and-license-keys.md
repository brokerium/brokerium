# ADR 0016: Editions and license keys

- Status: accepted
- Date: 2026-10-20

## Context

Brokerium gets a paid Enterprise edition next to the free Community edition. Everything that
is free in a published version stays free ([GOVERNANCE.md](../../GOVERNANCE.md)); Enterprise
only adds features. The Community binary must not contain paid code, the paid code stays
readable, and a license check must work in air-gapped networks without contacting anyone.

## Decision

- **Code.** Paid Go code lives in `ee/`, paid dashboard code in `web/src/ee/`, both under the
  Brokerium Commercial License ([ee/LICENSE](../../ee/LICENSE), SPDX
  `LicenseRef-Brokerium-Commercial`). Go files in `ee/` carry `//go:build enterprise`; the
  dashboard loads `web/src/ee` only when `VITE_EDITION=enterprise`, so the Community bundle
  does not include it. The Enterprise build is `go build -tags enterprise` with
  `VITE_EDITION=enterprise`.
- **Hooks.** `internal/edition` (Community code) names the edition and lists the catalog of
  Enterprise features (id, title, description), so the Community dashboard can show what
  Enterprise adds. `cmd/brokerium-server` calls an `enterprise` hook that is empty in Community
  builds and, in Enterprise builds, registers the `ee` API routes (`api.Extension`) and
  operation types (`ops.Engine.Register`).
- **License keys.** Text `brk-lic-1.<payload>.<signature>`:
  - `payload` is unpadded base64url of a JSON object: `v` (1), `id`, `kid`, `customer`,
    `email`, `edition` (`enterprise`), `features` (feature ids, or `*` for all), `limits`
    (reserved; empty means none), `issuedAt`, `notBefore`, `expiresAt` (RFC 3339) and
    `graceDays`;
  - `signature` is unpadded base64url of the Ed25519 signature over the ASCII text
    `brk-lic-1.<payload>`;
  - `kid` selects one of the public keys built into the Enterprise binary, so signing keys
    can be rotated. A `licensetest` build tag adds a test key for automated tests; release
    builds never have it.
- **States.** `none`, `invalid` (bad format, unknown `kid`, bad signature, or before
  `notBefore`), `valid` (between `notBefore` and `expiresAt`), `grace` (up to `graceDays` after
  `expiresAt`) and `expired`. A feature is allowed in `valid` and `grace`. The server evaluates
  the state at startup, when a key is stored and every hour, and stores only keys that are
  `valid` or in `grace`.
- **Where the key comes from.** `BROKERIUM_LICENSE` (the text) or `BROKERIUM_LICENSE_FILE` (a
  path, for example a mounted Secret) take precedence; otherwise the key stored in the database
  through the dashboard.
- **API.**
  - `GET /api/v1/license` (any signed-in user, both editions): the edition, the catalog with a
    `licensed` flag per feature, and in Enterprise builds the license (id, customer, features,
    `expiresAt`, `graceEndsAt`, state, source).
  - `PUT /api/v1/license` and `DELETE /api/v1/license` (admin, Enterprise builds only, audited
    as `license.update` and `license.delete`). Community builds answer 409 `edition_required`.
  - Enterprise routes and operation types answer 403 `license_required` without a license
    that covers their feature.
- **Expiry.** In `grace` the dashboard warns; after it, Enterprise features stop answering.
  Nothing free changes, and running Enterprise operations can still finish or be cancelled.
- **Distribution.** Binaries `brokerium-server-enterprise-<os>-<arch>` next to the Community
  ones and the image `bvaliev/brokerium-server-enterprise` with the same tags as
  `bvaliev/brokerium-server`. The image is public: the key, not the image, unlocks features.
  The Helm chart has `edition: community|enterprise` and `license.existingSecret`.
- **No telemetry.** The server never reports usage. Renewing keys online from the license
  center is optional and off by default.

## Consequences

- Users of the Community edition run exactly the published Community code.
- Anyone can read the paid code; using it in production needs a subscription, and changing
  it to bypass the key is a breach of the commercial license, not something the code can
  prevent.
- Every Enterprise feature declares its feature id and checks it in the API and in the
  operations engine; the dashboard shows locked features with an Enterprise badge.
