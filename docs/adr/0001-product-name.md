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
  2026-10-18).
- Every user-facing page that mentions Apache Kafka carries the ASF attribution sentence
  (see [TRADEMARKS.md](../../TRADEMARKS.md)). The Kafka logo is never used.

## Consequences

- A trademark search (WIPO, EUIPO, USPTO) and domain purchase (`brokerium.dev`,
  `brokerium.io`) remain open tasks before the public launch; `brokerium.com` is taken.
- The GitHub organization name `brokerium` is still free and should be reserved.
