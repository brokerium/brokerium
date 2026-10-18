# Naming and trademarks

Research date: 2026-10-15

Not legal advice.

Sources: research report C, section 4 (ASF trademark rules, translated from Russian); the trademark points in report D (see [licensing-research.md](licensing-research.md)); and the Brokerium naming record of 2026-10-15. "(unverified)" marks statements the researcher flagged as needing confirmation.

## Summary

- "Kafka" cannot be used in the product name. Acceptable forms: `<Name> for Apache Kafka` (at the ASF's discretion) and `<Name>, Powered by Apache Kafka`.
- The product is named **Brokerium**, full form "Brokerium for Apache Kafka" ([repository](https://github.com/bvaliev/brokerium)).

## ASF trademark rules for "Kafka"

### Not allowed

- The ASF FAQ explicitly prohibits Apache marks in the primary and secondary branding of third-party products. Neither "BigCo Kafka Thing" nor "BigCo distribution of Kafka" is allowed ([ASF trademark FAQ](https://www.apache.org/foundation/marks/faq/)).
- "Kafka" in the product name or logo.
- Apache marks in a second-level domain (for example, kafkafoo.io).

### Allowed

- `<Name>, Powered by Apache Kafka`. Conditions: the full name "Apache Kafka", a link to kafka.apache.org, attribution, and no suggestion of ASF endorsement.
- `<Name> for Apache Kafka`. The ASF guide for PMCs says the ASF "could allow" names like "FooBar Software for Apache Xerces", but not "FooBar Xerces" ([guide for PMCs](https://www.apache.org/foundation/marks/pmcs)). So `<Name> for Apache Kafka` is an accepted descriptive form, but it is the ASF's decision, not an automatic permission.
- (unverified) If this form is the official product name, it is better to confirm it in advance with trademarks@apache.org.

### Required attribution

Attribution of this form is required ([ASF Trademark Policy](https://www.apache.org/foundation/marks/)):

> Apache®, Apache Kafka®, Kafka® are trademarks of the ASF

### Precedents

- In January 2020 the Kafka PMC (Jun Rao) asked Yahoo to rename "Kafka Manager". The project became CMAK ("Cluster Manager for Apache Kafka") and removed the Kafka logo ([CMAK issue #713](https://github.com/yahoo/CMAK/issues/713)).
- Report D adds that the Apache Kafka team pushed for other renames too: Kafka Eagle became EFAK and KafkaCenter became KCenter ([example](https://github.com/yahoo/kafka-manager/issues/713)). It concluded that "KafkaClusterManagement" would be a risky product name.

## Brokerium naming record

### Decision

- Name: **Brokerium**. Product name: "Brokerium for Apache Kafka", following the `<Name> for Apache Kafka` form.
- Repository: [github.com/bvaliev/brokerium](https://github.com/bvaliev/brokerium).
- Chosen on 2026-10-15.

### Checks performed

- The GitHub account name `brokerium` was free.
- There were zero GitHub repositories and zero Docker Hub repositories named brokerium.
- The only web hit was a real-estate agency in Mexico (brokerium.shop).
- brokerium.com is registered by someone else.

### Rejected candidates

| Candidate | Reason |
|---|---|
| Streamwerk | StreamWerk ApS, a Danish SaaS company in streaming |
| Tidemark | an existing tool and a former company |
| Odradek | strong association with Death Stranding |
| Castellan | OpenStack Castellan |
| Raftwerk | too similar to the band Kraftwerk |
| Gregor | too common |
| Quorate, Headgate, Raftline | weaker candidates |

### Similar names

- A small AGPL project "Brokr" exists (`night-slayer18/brokr-platform`, 2 stars); low confusion risk. Report C also lists Brokr (AGPL-3.0) among the small projects in the [market landscape](market-landscape.md).

## Trademark search (2026-10-18)

Preliminary search by the agent, not a legal clearance. Sources:
[TMview](https://www.tmdn.org/tmview/) (EUIPO, EU national offices, USPTO, WIPO and others),
[WIPO Madrid Monitor](https://www3.wipo.int/madrid/monitor/en/), web search. TMview rejects
automated requests, so the search ran in a browser; a control search for "kafka" returned
results from EUIPO, CNIPA and INPI, so the empty result below is real.

- **"brokerium" (contains): no trademarks** in any office covered by TMview.
- **"brokeri" (contains): 110 marks**, mostly insurance and real-estate brokers in class 36.
  Earlier rights in classes 9 or 42 worth an attorney's look before filing in the EU:

| Mark | Office, number | Classes | Holder | Status |
| --- | --- | --- | --- | --- |
| BROKERIA the best way for you (figurative) | WIPO IR [1346458](https://www3.wipo.int/madrid/monitor/en/showData.jsp?ID=ROM.1346458), designates the EU; also SKIPO 1289-2016 | 9 (incl. "software (recorded programs)"), 16, 35, 36, 38, 39, 41 | Brokeria, a. s., Bratislava (insurance broker) | Expired 2026-06-10, within the 6-month renewal grace period (until about 2026-12-10) |
| brokerize | DPMA 3020222074889 | 9, 36, 42 | Guidants Trading GmbH | Registered |

- Not relevant: BROKERIQ (Australia, class 42, filed 2026), Brokeris (Chile, class 42),
  brokerito (EUIPO, classes 35-36), Brokerix (Switzerland, class 35), and the real-estate
  agency brokerium.shop in Mexico (class 36 business, no trademark found).
- Assessment for the owner: "broker" is descriptive for both brokerage and Kafka software, the
  endings differ, and the BROKERIA registration has been protected in the EU for more than five
  years, so in an opposition its holder would have to prove genuine use for software. Filing
  after the BROKERIA grace period ends shows whether it is renewed at all.

## Remaining before public launch

- [x] Trademark search in WIPO, EUIPO and USPTO (preliminary, see above).
- [x] Domain: `brokerium.kaufen` bought by the owner (Cloudflare DNS); `brokerium.dev`, `.io`
      and `.app` were still free on 2026-10-18; `brokerium.com` is registered since 2015.
- [x] GitHub organization `brokerium` created by the owner (2026-10-18).
- [ ] EU trademark application, plan in [ADR 0014](../adr/0014-name-protection-and-business-setup.md).
- [ ] Optional, deferred until the paid edition launches: email trademarks@apache.org to confirm
      the "for Apache Kafka" form. The form is used by Amazon Managed Streaming for Apache Kafka,
      Aiven for Apache Kafka and Kpow for Apache Kafka.

## Related points from the licensing research

From report D (details in [licensing-research.md](licensing-research.md)):

- A trademark outlives any fork: Valkey had to take a new name precisely because the Redis mark belongs to the company.
- Staged plan: on day 1, a brand without "Kafka", a trademark check and domains; with traction, a trademark application; before a sale, chain-of-title and trademark documents.
- Acquirers check the registration of the name and logo and the usage policy; domains, the GitHub organization and package names must belong to the company.
- Suggested `TRADEMARKS.md`: based on the Model Trademark Guidelines, with the note "Apache Kafka is a trademark of the ASF; the project is not affiliated with the ASF".
