# Licensing

Brokerium is **source-available**, not open source. The code is public so you can
read it, run it, audit it and adapt it to your needs, but it is licensed under the
[PolyForm Shield License 1.0.0](LICENSE), which does not allow building a product
that competes with Brokerium.

This page explains the license in plain words. If this page and the
[LICENSE](LICENSE) file ever disagree, the LICENSE file wins.

## What you can do

- Download, install and run Brokerium for free, including in production and inside
  companies of any size.
- Use it to operate your own Apache Kafka clusters, including clusters that serve
  your customers' applications.
- Read, study and modify the code for your own use, for example to patch a bug
  before an official fix is released.
- Share unmodified or modified copies with others for the purposes above, as long as
  you keep the LICENSE file and the `Required Notice` line.
- Propose improvements and bug fixes back to the project (see [CONTRIBUTING.md](CONTRIBUTING.md)).

## What you cannot do

- Provide any product or service that competes with Brokerium. The license is
  explicit that products compete even when they are free, and that marketing
  something as a practical substitute for Brokerium definitely competes. In
  practice this means:
  - no forks or rebrands of Brokerium distributed or promoted as an alternative;
  - no hosted or managed "Brokerium as a service" offered to third parties;
  - no Kafka management, monitoring, rebalancing or cluster-lifecycle product or
    service built on Brokerium code and offered to third parties.
- Remove or hide the LICENSE terms or the `Required Notice` line from copies you share.
- Use the Brokerium name or logo for your own products or modified versions (see
  [TRADEMARKS.md](TRADEMARKS.md)).

## Frequently asked questions

**Can my company use Brokerium at work for free?**
Yes. Internal use, including production use, is a permitted purpose.

**We are a consultancy or managed service provider. Can we use Brokerium to manage
Kafka clusters for our clients?**
Operating Kafka for clients as a service is close to the competing-use line. Please
ask for a commercial license; we are happy to work with service providers.

**Do old versions become open source after some time?**
No. Unlike BSL or FSL, the PolyForm Shield License has no automatic conversion
date. Every published version stays under the license it was published with.

**Can the license change in the future?**
Future versions may be published under different terms (for example a more
permissive license or an additional commercial edition). Versions that are already
published keep their license. See [GOVERNANCE.md](GOVERNANCE.md).

**What is the difference between Community and Enterprise?**
Brokerium Community is everything in this repository outside `ee/` and `web/src/ee/`,
free under the PolyForm Shield License, including the overview of any number of clusters.
Brokerium Enterprise adds paid features, starting with message tools (deleting records,
emptying topics, searching, exporting and copying messages). Its code lives in `ee/` and
`web/src/ee/` under the [Brokerium Commercial License](ee/LICENSE): you can read it, but
production use needs a subscription and a license key. The Community build and its
images contain none of that code. Features that were free in a published version never
move to Enterprise.

**Where is the commercial license?**
Enterprise subscriptions and other commercial licenses (OEM, service providers, special
terms) are available on request. Write to
[licensing@brokerium.kaufen](mailto:licensing@brokerium.kaufen); the maintainer replies
personally.

## License map

| Path | License |
| --- | --- |
| Everything in this repository unless stated otherwise | PolyForm Shield License 1.0.0 |
| `ee/` and `web/src/ee/` (Brokerium Enterprise) | [Brokerium Commercial License 1.0](ee/LICENSE) |
| Third-party dependencies (Go modules, npm packages, container base images) | Their own licenses; see the generated SBOM attached to each release |

Source files carry an SPDX header:

```text
SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
SPDX-License-Identifier: LicenseRef-Brokerium-Commercial   (files in ee/ and web/src/ee/)
```

## Trademarks

Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of
the Apache Software Foundation in the United States and/or other countries.
Brokerium is not affiliated with, endorsed by, or sponsored by the Apache Software
Foundation.
