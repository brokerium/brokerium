# ADR 0002: PolyForm Shield 1.0.0 with a CLA

- Status: accepted
- Date: 2026-10-15

## Context

The owner wants the code public and free to use, contributions only through his review,
no one able to fork the product and promote a competing version, and the ability to sell
the product or company later. Every OSI-approved license (Apache-2.0, AGPL-3.0, MPL-2.0)
allows forks, so none satisfies the no-competing-fork requirement. FSL and BSL convert to
open source after two to four years, which re-opens old versions for competing forks.

## Decision

- License the whole repository under the **PolyForm Shield License 1.0.0**, with a
  `Required Notice` and a `Licensor Line of Business` line in [LICENSE](../../LICENSE).
- Describe the product as **source-available**, never as "open source".
- Accept external code only under the [Individual CLA](../../CLA.md) or
  [Corporate CLA](../../CCLA.md), which grant the right to relicense under any terms and
  to assign the rights to a successor. CLA Assistant Lite records signatures on the
  `cla-signatures` branch.
- Use only dependencies under permissive or weak-copyleft licenses (MIT, BSD, Apache-2.0,
  ISC, MPL-2.0 unmodified). GPL, AGPL, SSPL, BSL and code without a license are not
  shipped in artifacts. CI checks dependency licenses and publishes an SBOM.
- Source files carry `SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0`.
- Commits use the owner's personal identity, never an employer email.

## Consequences

- Competing products, rebrands and hosted offerings by third parties are prohibited, even
  when free; the "Sales of Business" clause lets a buyer enforce this.
- Some enterprises need a legal review before adopting non-OSI software; Homebrew core and
  Docker's open-source program are not available.
- The owner can change the license of future versions at any time; published versions keep
  their license. Procedures are in [docs/legal/](../legal/).
