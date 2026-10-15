# Licensing research

Research date: 2026-10-15

Not legal advice.

Source: research report D (translated from Russian). The report collects public sources as of 2026-10-15 and advises finalizing the license, the CLA and any deals with an IP lawyer in your jurisdiction.

## Decision

- **Owner requirement:** nobody may fork Brokerium and promote the fork as an alternative or competing version. No OSI-approved license allows this restriction, so OSI licenses are ruled out, including the AGPL-3.0 recommended by this research.
- **Adopted:** PolyForm Shield 1.0.0 + CLA.
- **Records:** [docs/adr/0002-license.md](../adr/0002-license.md) and [docs/legal/licensing-strategy.md](../legal/licensing-strategy.md).
- The rest of this file is the original research. Its AGPL-3.0 recommendation is kept in [section 6](#6-research-recommendation-not-adopted) as a research recommendation (not adopted). PolyForm Shield was not among the licenses compared in the research.
- The sections on CLA vs DCO, GitHub enforcement, acquirer due diligence and relicensing mechanics are largely independent of the license choice.

## Research summary (original recommendation, not adopted)

For the goal "open source now, then EE or SaaS, then a sale", the report found the Grafana and Metabase model the best fit:

- the core under AGPL-3.0-only;
- SDK, CLI and Helm charts under Apache-2.0;
- later, an `ee/` directory under a commercial license;
- a CLA with the right to relicense, starting from the first external PR;
- an own trademark without the word "Kafka".

If maximum reach matters more: Apache-2.0 + `ee/` + the same CLA.

## 1. License comparison

OSI status per the [OSI license list](https://opensource.org/licenses).

| License | OSI | Adoption friction | Protection from clouds and competitors | Dual licensing and relicensing |
|---|---|---|---|---|
| [Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0) | yes | minimal; includes a patent grant | none | open core works; §4 allows releasing a derivative whole under other terms |
| MIT | yes | minimal | none | like Apache, but without a patent grant |
| MPL-2.0 | yes | low (file-level copyleft) | weak | other people's MPL files can be relicensed only with a CLA |
| AGPL-3.0 | yes | noticeable: some corporations ban it, Google entirely ([Google AGPL policy](https://opensource.google/documentation/reference/using/agpl-policy)) | medium: hosting is allowed, but modifications must be disclosed | the classic dual-licensing choice, but **only with a CLA** |
| [BSL 1.1](https://mariadb.com/bsl11/) | no | medium to high | strong (Additional Use Grant); each version becomes open source no later than 4 years after release | you sell a license for the prohibited use |
| [FSL-1.1](https://fsl.software/) | no (Fair Source) | medium | strong: "Competing Use" is prohibited; converts to Apache or MIT after 2 years | like BSL, but standardized |
| ELv2 ([FAQ](https://www.elastic.co/licensing/elastic-license/faq)) | no | medium | strong: prohibits offering a managed service and circumventing license keys; never converts to open source | the "source-available + license keys" model |
| SSPL | no (the OSI submission was withdrawn in 2019; [OSI](https://opensource.org/blog/licensereview032019)) | high: Fedora dropped MongoDB ([Fedora](https://www.fedoraproject.org/wiki/Changes/MongoDB_Removal)) | maximum | reputationally toxic |

### Open-core structures

- **GitLab and PostHog.** The core is MIT; the `ee/` directory is under a proprietary license, and `ee/` may be used in production only with a subscription ([PostHog ee/LICENSE](https://github.com/PostHog/posthog/blob/master/ee/LICENSE)).
- **Metabase** is the closest analog, also a self-hosted dashboard. Everything outside `enterprise/` is AGPL; inside it is a commercial license; an ICLA is mandatory ([Metabase LICENSE](https://github.com/metabase/metabase/blob/master/LICENSE.txt)).
- **Grafana.** The core is AGPL-3.0-only, SDKs and packages are Apache-2.0; the directory-to-license map is kept in [LICENSING.md](https://github.com/grafana/grafana/blob/main/LICENSING.md). Enterprise is a separate commercial build.

## 2. Precedents 2018–2026

| Case | What changed | Outcome |
|---|---|---|
| MongoDB, 2018 | AGPL → SSPL, including new patches to old branches | the OSI submission was withdrawn; Fedora and Debian dropped MongoDB ([press release](https://www.mongodb.com/company/newsroom/press-releases/mongodb-issues-new-server-side-public-license-for-mongodb-community-server)) |
| Confluent, 2018 | KSQL, Schema Registry and REST Proxy moved to the Confluent Community License; Kafka itself stayed Apache | Aiven released Karapace, an Apache-licensed replacement ([blog](https://aiven.io/blog/karapace-strengthens-schema-management)); IBM acquired Confluent for ~$11B in 2026-03 ([press release](https://www.confluent.io/press-release/ibm-completes-acquisition-of-confluent/)) |
| CockroachDB | 2019: Apache → BSL; 2024: a proprietary license, free for revenue < $10M and with mandatory telemetry | criticized for drifting toward proprietary ([FAQ](https://docs.cockroachlabs.com/docs/stable/licensing-faqs)) |
| Sentry | 2019: BSD → BSL; 2023: its own FSL | SDKs stayed MIT and Apache ([blog](https://blog.sentry.io/introducing-the-functional-source-license-freedom-without-free-riding/)) |
| Elastic | 2021: Apache → ELv2 and SSPL | AWS forked 7.10.2 into OpenSearch; in 2024 Elastic added AGPL ([AWS](https://aws.amazon.com/blogs/opensource/stepping-up-for-a-truly-open-source-elasticsearch/), [Elastic](https://www.elastic.co/blog/elasticsearch-is-open-source-again)) |
| Grafana | 2021: Apache → AGPL; libraries and plugins stayed Apache; CLA based on the Apache ICLA | no major fork; ARR > $600M in 2026-08 ([blog](https://grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3/), [press release](https://grafana.com/press/2026/08/26/grafana-labs-crosses-10000-customer-milestone-as-ai-adoption-accelerates-growth-across-the-platform/)) |
| MinIO | 2021: Apache → AGPL; 2025: admin UI and binaries removed from the free edition; 2026-02: repository archived | trust lost; the community forked it ([repo](https://github.com/minio/minio)) |
| HashiCorp | 2023: MPL → BSL; SDKs and APIs stayed MPL | OpenTofu fork (Linux Foundation, in the CNCF since 2025); IBM acquired the company for $6.4B in 2025-02 ([blog](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license), [IBM](https://newsroom.ibm.com/2025-02-27-ibm-completes-acquisition-of-hashicorp,-creates-comprehensive,-end-to-end-hybrid-cloud-platform)) |
| Redis | 2024: BSD → RSALv2 and SSPL | Valkey appeared 8 days later (Linux Foundation; AWS, Google, Oracle); AGPL added in 2025; the CEO admitted the change "hurt our relationship with the Redis community" ([Valkey](https://valkey.io/topics/history/), [Redis](https://redis.io/blog/agplv3/)) |
| Redpanda | BSL: may not be sold as a streaming service; the code converts to Apache after 4 years; enterprise features, including SSO and RBAC in Console, require a license key | see [docs](https://docs.redpanda.com/current/get-started/licensing/overview/) |
| n8n | 2022: Apache + Commons Clause → Sustainable Use License | valued at $2.5B in 2025-10 ([blog](https://blog.n8n.io/series-c/)) |
| Cal.com | AGPL + `ee/`; in 2026-04 the code was closed and the public repository became Cal.diy under MIT | see [blog](https://cal.com/blog/cal-diy-open-source-to-closed-source) |
| Supabase | Apache-2.0 | valued at $10.5B in 2026-06 ([press release](https://www.prnewswire.com/news-releases/supabase-raises-500m-at-10-5b-to-accelerate-lead-in-agentic-infrastructure-302791787.html)) |

### Lessons

1. **Tightening a permissive license after success** leads to hyperscaler-backed forks within days or weeks: OpenSearch, Valkey, OpenTofu. Elastic and Redis later partially rolled back by adding AGPL. Liquibase moved from Apache to FSL in 2025 and got a backlash, and CNCF projects such as Keycloak started looking for a replacement ([issue](https://github.com/keycloak/keycloak/issues/43391)). A protective license has to be chosen at the start.
2. **Grafana.** Its move from Apache to AGPL was the quietest of all: the license stayed OSI-approved, and the libraries stayed Apache.
3. **A trademark outlives any fork.** Valkey had to take a new name precisely because the Redis mark belongs to the company.
4. **The license did not prevent expensive deals.** They happened with restrictive licenses (HashiCorp, Confluent) and permissive ones (Supabase). What decides is revenue and clean ownership of the code.
5. **Do not promise "forever", and do not hand the repository and the trademark to a foundation.** AWS publicly quoted Elastic's 2018 promise "we never will". In 2025 Synadia tried to take NATS out of the CNCF and move it to BSL, but in the end transferred the marks to the Linux Foundation ([CNCF](https://www.cncf.io/announcements/2025/05/01/cncf-and-synadia-align-on-securing-the-future-of-the-nats-io-project/)).
6. **Do not cut the free version retroactively, as MinIO did.** Publish the boundary between free and paid in advance. In the Kafka UI market, the Apache-licensed Kafbat UI and AKHQ are free, and people pay for SSO, RBAC, audit and governance ([comparison](https://www.conduktor.io/compare/kafka-ui-tools)).

## 3. CLA vs DCO and contribution control

### Defaults and what DCO covers

- **Default.** On GitHub the inbound = outbound principle applies: a contribution falls under the repository's license, and the copyright stays with the author. A separate CLA takes precedence ([GitHub ToS D.6](https://docs.github.com/en/site-policy/github-terms/github-terms-of-service)).
- **What DCO is enough for.** The [DCO](https://developercertificate.org/) only certifies that the author has the right to contribute the code. That is enough with a permissive core if you write `ee/` yourself: GitLab has run on MIT + DCO since 2017 ([GitLab blog](https://about.gitlab.com/blog/gitlab-switches-to-dco-license/)).
  - DCO does not let you sell a commercial license to AGPL code that contains other people's contributions.
  - It does not let you move other people's AGPL or MPL code to BSL, FSL or a proprietary license.
  - Without a CLA you are yourself bound by the AGPL with respect to other people's code in your own SaaS or EE.
  - With an Apache or MIT core, future versions can be released without a CLA, but with a CLA the chain of title is cleaner for an acquirer.

### Assignment vs license

- Full copyright assignment gives maximum control but also maximum friction. In some countries, Germany for example, it is not possible at all.
- More practical: a broad non-exclusive license modeled on the Apache ICLA: perpetual, irrevocable, with the right to sublicense and a patent license.
- Grafana calls such a CLA a balance between the contributor's interests and its "rights to relicense" ([Grafana CLA](https://grafana.com/docs/grafana/latest/developer-resources/contribute/cla/)).
- Add an explicit right to distribute the code under any license, including a proprietary one, and the right to transfer the CLA to a successor (your company, an acquirer). Contributors employed by companies need a CCLA.

### Templates

- [Apache ICLA and CCLA](https://www.apache.org/licenses/contributor-agreements.html).
- [Harmony](https://www.harmonyagreements.org/).
- [contributoragreements.org](https://contributoragreements.org/ca-cla-chooser/).
- The FSFE FLA does not fit: it is a fiduciary agreement that obliges keeping the code free.

### Tools

- cla-assistant.io is hosted by SAP; Redpanda, for example, uses it.
- [CLA Assistant Lite](https://github.com/contributor-assistant/github-action): a GitHub Action that stores signatures as a JSON file in a separate private repository. The signatures branch must not be protected, and a job on `pull_request_target` must not check out code from the PR.
- [EasyCLA](https://docs.linuxfoundation.org/lfx/easycla) from the Linux Foundation is overkill for a solo project.

### GitHub enforcement settings: accepting contributions only with the owner's approval

- Give nobody write access: external contributors submit code only via fork and PR. The owner of a personal repository can merge into a protected branch without approvals ([permission levels](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/repository-access-and-collaboration/permission-levels-for-a-personal-account-repository)).
- Ruleset on `main`: Require a pull request, required CI and CLA checks, Block force pushes, Restrict deletions ([available rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)).
- `.github/CODEOWNERS` with the line `* @you` and "Require review from Code Owners" enabled. This becomes useful once there are collaborators.
- "Restrict who can push" only works in organization repositories, including public repositories of a free organization ([protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)). So it is worth creating a free organization right away.
- In Actions, enable "Require approval for all external contributors" ([Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)).

## 4. What acquirers check

### Scale of the problem

Black Duck OSSRA 2026 analyzed 947 codebases from 197 M&A deals ([OSSRA](https://www.blackduck.com/resources/analyst-reports/open-source-security-risk-analysis.html), [M&A numbers](https://www.blackduck.com/content/dam/black-duck/en-us/whitepapers/wp-risk-ma-numbers.pdf)):

- License conflicts were found in 68% of codebases and 94% of deals.
- 69% of deals had code with no license or a non-standard license.
- The most frequent conflict is CC-BY-SA (54% of deals): snippets from Stack Overflow, and now also from AI assistants.
- Consequences: escrow, a lower price, a delayed deal.

### What they look at

Per [MC Law](https://mclaw.io/blog/ip-due-diligence-checklist-for-mergers-and-acquisitions--a-practical-checklist) and [Morgan Lewis](https://www.morganlewis.com/blogs/sourcingatmorganlewis/2026/06/open-source-software-common-areas-of-inquiry-in-m-and-a-due-diligence):

1. **Chain of title.** Assignment of rights from the founder to the company, including code written before incorporation. Assignments from employees and contractors (in the US, a contractor's code is not work-for-hire by default). A CLA for every external commit.
2. **Inbound licenses.** An SBOM (SPDX or CycloneDX), an independent SCA scan, copyleft and AGPL analysis, compliance with NOTICE requirements, a written open-source policy.
3. **Outbound licenses.** Consistency between LICENSE, LICENSING and the commercial EULA.
4. **Trademarks and assets.** Registration of the name and logo, a usage policy. Domains, the GitHub organization and package names must belong to the company.

### Kafka specifics

- The ASF prohibits "Kafka" in the name of a third-party product; "X for Apache Kafka" or "Powered by Apache Kafka" is acceptable ([ASF FAQ](https://www.apache.org/foundation/marks/faq/)).
- The Apache Kafka team has pushed for renames: Kafka Manager became CMAK, Kafka Eagle became EFAK, KafkaCenter became KCenter ([example](https://github.com/yahoo/kafka-manager/issues/713)).
- Therefore the name "KafkaClusterManagement" is risky for a product. The product is now named Brokerium; see [naming-and-trademarks.md](naming-and-trademarks.md).

## 5. Relicensing mechanics

### What you can do

- Change the license of future commits and releases of code you own or have a CLA for. MongoDB applied SSPL even to new patches of old branches.

### What you cannot do

- Revoke the license of versions already published: they stay under the old license forever, and a fork starts from the last open version. OpenSearch grew out of 7.10.2, Valkey out of 7.2.4, OpenTofu out of the last MPL version of Terraform.
- Confluent ([FAQ](https://www.confluent.io/confluent-community-license-faq/)) and Liquibase ([blog](https://www.liquibase.com/blog/liquibase-community-for-the-future-fsl)) explicitly confirm this.
- Relicense third-party dependencies.
- Relicense other people's AGPL or MPL code without a CLA: that requires the author's consent or a rewrite.

### Prerequisites

- The CLA covers all code; whatever is not covered is removed or rewritten.
- Dependencies are compatible with the new license.
- The trademark belongs to the legal entity.
- It is decided what stays permissive: usually SDKs and clients, as at HashiCorp, Elastic and Sentry.

### Procedure

1. Audit: reconcile `git log` with the CLA signatures and build an SBOM.
2. Update the license text, LICENSING.md, SPDX headers, and the metadata of packages, images and Helm charts. Check the "open source" wording in the README: Liquibase had to fix it ([issue](https://github.com/liquibase/liquibase/issues/7374)).
3. Tag the last release under the old license and announce the version and effective date of the new one.
4. Publish a post and an FAQ ("who is affected and who is not", with examples) in advance. The OpenTofu manifesto reproached HashiCorp precisely for "little or no advance notice" ([manifesto](https://opentofu.org/manifesto/)).
5. During a transition period, ship security fixes for the last branch under the old license; update CONTRIBUTING and the CLA.

### Preparing in advance

Write into GOVERNANCE.md: "future versions may be released under a different license; published versions keep theirs."

## 6. Research recommendation (not adopted)

> **Not adopted.** The owner's requirement that nobody may promote a fork as a competing version rules out AGPL-3.0 and every other OSI license; Brokerium uses PolyForm Shield 1.0.0 + CLA (see [Decision](#decision)). This section is kept for the record. Items that do not depend on the license choice may still be useful; [docs/legal/licensing-strategy.md](../legal/licensing-strategy.md) records what was actually adopted.

### Choice

- AGPL-3.0-only for the control plane and the web UI.
- Apache-2.0 for the SDK, CLI, Terraform provider, Helm charts and OpenAPI schemas.
- An ICLA-based CLA with the right to relicense and to transfer to a successor, from the first external PR.
- An `ee/` directory once there is demand.

### Why

- **Real OSI open source.** There is no need to tighten the license later.
- **Stronger protection than Apache.** A Kafka SaaS vendor cannot embed your UI into a managed service with closed modifications. And companies that ban AGPL become buyers of a commercial license.
- **The CLA keeps options open in both directions.** Loosening the license to Apache is easy and is perceived positively; tightening future versions to FSL or BSL is legally possible.
- **Value to an acquirer.** AGPL together with a CLA means that only your company can issue the code under proprietary terms. That is exclusive IP value.
- **Cost of the decision.** Some corporations will not allow AGPL, and the CLA will deter some contributors. Mitigations: Apache for the integration parts, and an FAQ explaining that running the software unmodified does not require disclosing code.
- **Alternative.** If reach matters more (Kafbat and AKHQ are free and Apache-licensed), take Apache-2.0 + `ee/` + the same CLA, and do not plan to tighten the license beyond AGPL.

### Stage plan

- **Day 1:**
  - A brand without "Kafka" (for example, `<Name> for Apache Kafka`), a trademark check, domains.
  - A free GitHub organization under your personal account, and the files from the list below.
  - Ruleset, CODEOWNERS, CLA bot.
  - In CI: an SBOM and a dependency license check. MIT, BSD, Apache and MPL pass automatically; GPL, AGPL, SSPL, BSL, CC-BY-SA and unlicensed code are reviewed manually.
  - SPDX headers following [REUSE](https://reuse.software/); no copy-paste from Stack Overflow and no unreviewed AI-generated code.
- **With traction:**
  - A legal entity and an agreement assigning IP to the company: code written before incorporation, domains, the trademark, the GitHub organization, the rights under the CLA.
  - A trademark application.
  - IP assignment agreements with everyone who writes code.
  - An open-source policy.
- **At commercialization:**
  - An `ee/` directory under a commercial license with a license key; an EULA.
  - A commercial license for those who cannot use AGPL; for SaaS, ToS and a DPA.
  - A public boundary between free and paid: SSO, RBAC, audit and governance are paid; nothing is taken away from the free version.
  - If free-riding through hosting appears, move future versions to FSL using the procedure in section 5.
- **Before a sale:**
  - A CLA coverage report.
  - An SBOM and a proactive third-party audit with conflicts resolved.
  - Chain-of-title and trademark documents.
  - The license history and communications history.

### Files in the repository from day one

- `LICENSE` (the full AGPL-3.0 text) and `LICENSING.md`: a "directory → license" map and a contact for the commercial license.
- `NOTICE`: copyright and dependency attributions.
- `CLA.md`, `CCLA.md`, `.github/workflows/cla.yml`.
- `CONTRIBUTING.md`: fork and PR, signing the CLA, the maintainer's right to reject any PR.
- `.github/CODEOWNERS`, `.github/PULL_REQUEST_TEMPLATE.md` with the checkboxes "CLA signed" and "the code is mine, not copied", and issue templates.
- `CODE_OF_CONDUCT.md` based on [Contributor Covenant 3.0](https://www.contributor-covenant.org/version/3/0/code_of_conduct/).
- `SECURITY.md`: a private channel via GitHub Private Vulnerability Reporting.
- `TRADEMARKS.md` based on the [Model Trademark Guidelines](http://modeltrademarkguidelines.org/index.php/Model_Trademark_Guidelines), with the note "Apache Kafka is a trademark of the ASF; the project is not affiliated with the ASF".
- `GOVERNANCE.md`: a model with a single deciding maintainer (BDFL), the decision process, the license policy.
- `README.md` with a License section, and `CHANGELOG.md`.
