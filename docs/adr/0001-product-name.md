# ADR 0001: Product name "Brokerium"

- Status: accepted
- Date: 2026-10-15

## Context

The product needs a name for the repository, container images, CLI binaries and a future
trademark. The Apache Software Foundation does not allow third parties to use "Kafka" in
product names (Yahoo's "Kafka Manager" had to be renamed to CMAK in 2020). The allowed
descriptive forms are "<Name> for Apache Kafka" and "<Name>, Powered by Apache Kafka".

## Decision

- The product is called **Brokerium**; the descriptive form is "Brokerium for Apache Kafka".
- Binaries: `brokerium-server`, `brokerium-agent`. Images: `bvaliev/brokerium-server`,
  `bvaliev/brokerium-agent`. Go module: `github.com/brokerium/brokerium` (it was
  `github.com/bvaliev/brokerium` until the repository moved to the `brokerium` organization on
  2026-10-18, [ADR 0014](0014-name-protection-and-business-setup.md)).
- Logo (added 2026-10-18): three brokers of equal height in a ring with a partition being
  placed, in the dashboard's indigo-to-violet gradient; the name in Inter Display Bold as
  outlines. Files and rules: [docs/brand](../brand/).
- Every user-facing page that mentions Apache Kafka carries the ASF attribution sentence
  (see [TRADEMARKS.md](../../TRADEMARKS.md)). The Kafka logo is never used.

Name checks are recorded in [docs/research/naming-and-trademarks.md](../research/naming-and-trademarks.md).

## Consequences

- A trademark search (WIPO, EUIPO, USPTO) and domain purchase (`brokerium.dev`,
  `brokerium.io`) remain open tasks before the public launch; `brokerium.com` is taken.
- The GitHub organization name `brokerium` is still free and should be reserved.
