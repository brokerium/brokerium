# ADR 0014: Name protection, domain and business setup

- Status: accepted
- Date: 2026-10-18

## Context

Brokerium v0.1.0 is public. The license protects the code but not the name, the repository lives
in the owner's personal GitHub account, and there is no legal entity yet. The owner lives in
Czechia and is employed by a company unrelated to Kafka tooling. Research:
[naming-and-trademarks.md](../research/naming-and-trademarks.md),
[czech-business-setup.md](../research/czech-business-setup.md).

## Decision

- **GitHub:** the organization `brokerium` holds the product's repositories, so they can be handed
  over with the business. The website lives in a separate private repository there.
- **Domain:** `brokerium.kaufen` (DNS on Cloudflare) is the primary domain for the website and
  email. Licensing and corporate CLA requests go to `licensing@brokerium.kaufen`, forwarded by
  Cloudflare Email Routing; until it works, they go to GitHub Discussions.
- **Website hosting:** Cloudflare Pages from the private repository, on the free plan.
- **Legal form:** the owner registers as an OSVČ (sole trader) before the first paid contract or
  the trademark filing; an s.r.o. follows when the business needs limited liability or a share
  sale. Moving the code into a company then takes an exclusive licence from the author, because
  Czech law does not allow transferring an author's economic rights.
- **Trademark:** an EU trade mark for the word BROKERIUM in classes 9 and 42, filed in early 2027:
  after the renewal grace period of the similar mark BROKERIA (class 9) ends around 2026-12-10,
  and with an SME Fund voucher if the 2027 fund opens. Within 6 months of filing, extend it
  through the Madrid system to the markets that matter, keeping the EU filing date. An attorney
  reviews the similar marks before filing.
- **Apache trademarks:** keep the "Brokerium for Apache Kafka" form and the attribution; asking
  trademarks@apache.org for confirmation is deferred until the paid edition is marketed.

## Consequences

- Until the EU mark is filed, the name has no registered protection; watch new filings in
  classes 9 and 42.
- Trademark, domain and licences are held by the owner personally while he is an OSVČ and must be
  moved to a company before a share sale.
- Repository links change when the repository moves to the organization; GitHub redirects the old
  URLs.
