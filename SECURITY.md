# Security Policy

Brokerium has administrative access to Apache Kafka clusters, so security reports are
handled with priority.

## Reporting a vulnerability

Do not open a public issue. Report privately through GitHub:
[Report a vulnerability](https://github.com/brokerium/brokerium/security/advisories/new).
If you cannot use GitHub, write to
[security@brokerium.kaufen](mailto:security@brokerium.kaufen).

Please include:

- affected Brokerium version or commit;
- affected component (server, agent, dashboard, installer);
- steps to reproduce or a proof of concept;
- the impact you expect.

## What to expect

- Acknowledgement within 3 business days.
- An initial assessment within 10 business days.
- A fix or mitigation plan agreed with you, followed by a coordinated advisory. We
  credit reporters who want to be credited.

## Supported versions

Until version 1.0.0, only the latest release receives security fixes.

## Scope

In scope: the Brokerium server, agent, dashboard, installers, container images and
Helm chart published from this repository.

Out of scope: vulnerabilities in Apache Kafka itself (report them to the
[Apache Kafka project](https://kafka.apache.org/project-security)), and issues that
require an attacker to already have administrator access to Brokerium.

## Safe harbor

We will not pursue legal action against researchers who act in good faith, avoid
privacy violations and service disruption, and give us reasonable time to fix the
issue before disclosure.
