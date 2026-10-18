# Плейбук смены лицензии / Relicensing playbook

> **Abstract (English).** Step-by-step scenarios for changing Brokerium's license: (A) opening up
> new versions under FSL-1.1-ALv2, AGPL-3.0 or Apache-2.0, (B) launching a commercial edition and
> (C) a change requested by an investor or a buyer. Each scenario lists preconditions, exact
> steps, an English announcement template, a timeline and what cannot be done. Published
> versions always keep the license they were released with.

Актуально на 15.10.2026. Не юридическая консультация, см. [README](README.md).

## 1. Правила для любого сценария

Можно менять лицензию будущих коммитов и релизов кода, которым вы владеете или на который есть
CLA, параллельно продавать коммерческие лицензии (Shield этого не запрещает) и выпускать разные
части под разными лицензиями с картой «путь → лицензия» в LICENSING.md, как у
[Grafana](https://github.com/grafana/grafana/blob/main/LICENSING.md).

Нельзя:

- отозвать лицензию у опубликованных версий. Они навсегда остаются под своей лицензией, а форк
  стартует от последней из них: OpenSearch вырос из Elasticsearch 7.10.2, Valkey — из Redis
  7.2.4, OpenTofu — из последней MPL-версии Terraform. Это прямо подтверждают
  [Confluent](https://www.confluent.io/confluent-community-license-faq/) и
  [Liquibase](https://www.liquibase.com/blog/liquibase-community-for-the-future-fsl);
- перелицензировать сторонние зависимости: они остаются под своими лицензиями;
- перелицензировать чужой вклад без CLA: нужно согласие автора или переписанный код;
- нарушить обещания [GOVERNANCE.md](../../GOVERNANCE.md): опубликованные версии сохраняют
  лицензию, бесплатные функции не уходят за paywall.

Ослабление лицензии обычно встречают позитивно, а ужесточение после успеха приводит к форкам за
дни или недели. Brokerium сразу стартовал с защитной лицензии, поэтому вероятны сценарии A и B.

## 2. Предусловия

### 2.1 Аудит покрытия CLA

1. Выгрузите авторов коммитов и соавторов, авторов смёрдженных PR и подписи CLA:
   ```bash
   git log --format='%aN <%aE>' | sort -u > authors.txt
   git log --format='%(trailers:key=Co-authored-by,valueonly)' | sort -u >> authors.txt
   gh pr list --repo brokerium/brokerium --state merged --limit 1000 \
     --json number,author --jq '.[] | "\(.author.login) #\(.number)"' > pr-authors.txt
   git fetch origin cla-signatures
   git show origin/cla-signatures:signatures/version1/cla.json \
     | jq -r '.signedContributors[] | "\(.name) \(.created_at) PR#\(.pullRequestNo)"' > cla.txt
   ```
2. Сопоставьте списки. У каждого внешнего автора, кроме `bvaliev` и ботов из allowlist, должна
   быть подпись не позже первого смёрдженного PR. Email из `git log` сверяйте с логином по PR.
   Сотрудникам компаний нужен ещё CCLA, в списке Designated Employees которого они есть.
3. Проверьте код, пришедший вне PR: фрагменты из issues и discussions покрыты CLA, только если
   их автор подписал CLA.
4. Закройте пробелы: попросите автора подписать CLA. Если не получается, удалите вклад или
   перепишите его сами и отметьте это в сообщении коммита.
5. Сохраните отчёт: дата, диапазон коммитов, число авторов, пробелы и что с ними сделано. Отчёт
   пойдёт в data room.

### 2.2 Чек-лист предусловий

- [ ] Отчёт о покрытии CLA без открытых пробелов.
- [ ] SBOM текущей версии собран, совместимость каждой зависимости с новой лицензией проверена
      вместе с юристом.
- [ ] Заявка на товарный знак Brokerium подана, владелец знака — юрлицо.
- [ ] IP и права по CLA переданы компании по договору уступки (раздел 7 CLA позволяет это без
      согласия авторов); компания будет лицензиаром новых версий.
- [ ] Решено, какие компоненты под какой лицензией: сервер, агент, дашборд, Helm-чарт, будущие
      SDK и CLI. HashiCorp, Elastic и Sentry при смене оставили SDK пермиссивными.
- [ ] Юрист проверил новый текст лицензии, FAQ и анонс.

## 3. Общая процедура

`vX.Y.Z` — последний релиз под старой лицензией, `vX.(Y+1).0` — первый релиз под новой.

1. **Зафиксируйте последний релиз под старой лицензией**, а в его заметках напишите «последний
   релиз под PolyForm Shield License 1.0.0»:
   ```bash
   git tag -a license/shield-final 'vX.Y.Z^{}' -m "Last release under PolyForm Shield 1.0.0"
   git branch release-X.Y vX.Y.Z   # security-фиксы под старой лицензией
   git push origin license/shield-final release-X.Y
   ```
2. **Подготовьте ветку со сменой** и не мёрджите её до даты вступления:
   - `LICENSE` — новый текст; строку `Required Notice` перенесите в формат новой лицензии (для
     Apache-2.0 — в `NOTICE`);
   - `LICENSING.md` — новая карта лицензий с фразой «версии до vX.Y.Z включительно остаются под
     PolyForm Shield 1.0.0» и новый FAQ;
   - SPDX-заголовки в исходниках; команда трогает только код, LICENSING.md и `docs/legal/`
     правьте вручную, сохраняя историю:
     ```bash
     rg -l 'LicenseRef-PolyForm-Shield-1.0.0' -g '*.go' -g '*.ts' -g '*.tsx' -g '*.proto' \
       -g '*.sql' | xargs sed -i '' 's/LicenseRef-PolyForm-Shield-1.0.0/<NEW-SPDX-ID>/'
     ```
   - `NOTICE`, раздел License в `README.md`, политика лицензий в `GOVERNANCE.md`, правило про
     SPDX в `CONTRIBUTING.md`, запись в `CHANGELOG.md`, поле `license` в `web/package.json`;
   - `LABEL org.opencontainers.image.licenses="<NEW-SPDX-ID>"` в Dockerfile сервера и агента,
     описания `bvaliev/brokerium-server` и `bvaliev/brokerium-agent` на Docker Hub;
   - аннотация лицензии в `Chart.yaml` (например, `artifacthub.io/license`), README чарта и
     новая версия чарта; описание репозитория на GitHub и сайт;
   - формулировки (`rg -n -i 'open[ -]source'`): «open source» допустимо только после перехода
     на OSI-лицензию, при FSL пишем «source-available» или «Fair Source». Liquibase исправляла
     их уже после смены ([issue](https://github.com/liquibase/liquibase/issues/7374)).
3. **Напишите FAQ «кого касается, а кого нет»** с примерами: какие версии затронуты, нужно ли
   пользователям что-то делать, что с форками и CLA, где коммерческая лицензия, что со знаком.
4. **Объявите заранее** версию и дату вступления: закреплённая Discussion, README, CHANGELOG,
   рассылка. Манифест OpenTofu упрекал HashiCorp именно в «little or no advance notice»
   ([манифест](https://opentofu.org/manifesto/)).
5. **В дату вступления** смёрджите ветку, выпустите vX.(Y+1).0, проверьте метки образов и чарт.
6. **В переходный период** выпускайте security-фиксы в `release-X.Y` до объявленной даты.
7. **Обновите CONTRIBUTING и CLA**, если меняются правила вкладов. Для новой версии CLA укажите
   новый путь подписей в `.github/workflows/cla.yml` (`signatures/version2/cla.json`).
8. **Сохраните доказательства** в ADR и data room: обоснование, отчёт CLA, SBOM, анонс, дату и
   ссылку на коммит.

## 4. Сценарий A: открыть код шире ради роста

**Когда:** внедрение регулярно упирается в лицензионные проверки, нужен Homebrew core или
программа Docker для open-source проектов, пермиссивный конкурент забирает пользователей.

| Новая лицензия | Что получают пользователи | Что остаётся у владельца |
| --- | --- | --- |
| FSL-1.1-ALv2 | каждая версия через 2 года становится Apache-2.0 | 2 года запрета Competing Use, товарный знак |
| AGPL-3.0 | open source по OSI, легальные форки | форки обязаны раскрывать код, в том числе при SaaS; коммерческие лицензии для тех, кому AGPL запрещён; знак |
| Apache-2.0 | максимальный охват, минимальное трение | только знак, темп разработки и платная редакция |

Grafana при переходе с Apache на AGPL осталась в OSI и оставила библиотеки под Apache, поэтому
её смена прошла тише всех ([блог](https://grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3/)).

**Предусловия** — раздел 2, плюс:
- [ ] Заявка на знак подана: после открытия знак — главная защита от форков.
- [ ] Решено, на чём зарабатывать после открытия: платная редакция, поддержка, SaaS.
- [ ] Для AGPL готовы условия коммерческой лицензии для компаний, которым AGPL запрещён.
- [ ] Для FSL проверен идентификатор: `FSL-1.1-ALv2`, если он есть в
      [списке SPDX](https://spdx.org/licenses/), иначе `LicenseRef-FSL-1.1-ALv2`.

**Шаги:**
1. Записать решение и выбранную лицензию в ADR.
2. Пройти общую процедуру (раздел 3).
3. Для AGPL добавить в FAQ: запуск без изменений не обязывает раскрывать код, а изменения нужно
   раскрыть пользователям сетевого сервиса.
4. Для OSI-вариантов после релиза подать заявки в Homebrew core и программу Docker для
   open-source проектов.

**График:** T−60 дней — решение, юрист, аудит CLA, SBOM; T−30 — анонс и FAQ; T0 — релиз под
новой лицензией; T+90 — последние security-фиксы для `release-X.Y`.

**Нельзя:**
- закрыть обратно версии, выпущенные под новой лицензией (под FSL каждая через 2 года станет
  Apache-2.0);
- открыть код без CLA или сторонний код под чужой лицензией;
- передавать фонду репозиторий и знак: Synadia в 2025 пыталась вывести NATS из CNCF и перевести
  на BSL, но в итоге передала знаки Linux Foundation
  ([CNCF](https://www.cncf.io/announcements/2025/05/01/cncf-and-synadia-align-on-securing-the-future-of-the-nats-io-project/)).

**Шаблон анонса** (`<...>` — подставить своё, `[...]` — фраза по желанию):
```text
Title: Brokerium <vX.(Y+1).0> moves to <NEW LICENSE>

Starting with Brokerium <vX.(Y+1).0>, planned for <YYYY-MM-DD>, new releases will be published
under <NEW LICENSE> instead of the PolyForm Shield License 1.0.0. [Why: "Many teams told us
that their legal review blocks non-OSI licenses."]

What changes: <"Brokerium becomes open source under Apache-2.0." | "Each release becomes
available under Apache-2.0 two years after it is published.">

What does not change:
- Brokerium stays free to use, including in production.
- Releases up to and including <vX.Y.Z> stay under the PolyForm Shield License 1.0.0. Nothing
  is revoked, and you do not need to do anything.
- "Brokerium" and its logo remain trademarks of <OWNER>. Forks must use a different name.
- Contributions are still accepted under the same CLA.

Timeline: <date> announcement and FAQ (<link>); <date> <vX.(Y+1).0> released; security fixes
for <vX.Y> until <date>. Questions: <link to the Licensing discussion category>.

<Name>, maintainer of Brokerium
```

## 5. Сценарий B: коммерческая редакция

**Когда:** есть спрос на функции для больших команд или запросы OEM и сервис-провайдеров.
Community-редакция остаётся под Shield, лицензию ядра менять не нужно.

**Модель:**
- Граница бесплатного и платного публикуется до первой строки платного кода. Кандидаты из
  плана: SSO, расширенные роли, экспорт аудита, ребаланс по расписанию и self-healing,
  ИИ-ассистент через MCP, облачная версия. Роли и аудит из фазы 4 будут бесплатными, поэтому
  границу проводим до её релиза. Kafbat UI и AKHQ бесплатны (Apache), а платят на рынке за SSO,
  RBAC, аудит и governance ([сравнение](https://www.conduktor.io/compare/kafka-ui-tools)).
- Где код: каталог `ee/` со своим `ee/LICENSE`, как у
  [PostHog](https://github.com/PostHog/posthog/blob/master/ee/LICENSE) и GitLab или
  `enterprise/` у [Metabase](https://github.com/metabase/metabase/blob/master/LICENSE.txt)
  (одна кодовая база, но платный код виден всем), или приватный репозиторий с отдельной
  сборкой, как у Grafana Enterprise (код закрыт, но сборок две).
- Виды лицензий: Enterprise-подписка на функции по ключу; OEM для вендоров, которые встраивают
  Brokerium в свой продукт или managed Kafka; лицензия сервис-провайдера для MSP и консалтинга;
  индивидуальные условия. OEM и лицензии сервис-провайдеров можно продавать и без `ee/`, но
  первый такой договор проверяет юрист.
- Лицензионный ключ: подписанный файл с клиентом, редакцией, лимитами и сроком, проверка офлайн
  без обязательной связи с нашим сервером (важно для закрытых контуров), льготный период после
  истечения. Закрытый ключ подписи хранится вне репозитория и передаётся покупателю при продаже.
- Документы готовит юрист: EULA с запретом обхода ключей, order form, условия поддержки и SLA,
  OEM- и MSP-договоры, для SaaS — ToS и DPA.

**Предусловия** — раздел 2, плюс:
- [ ] Юрлицо создано и владеет IP: договоры с клиентами подписывает компания.
- [ ] Покрытие CLA полное: продавать коммерческую лицензию на чужие вклады можно только с CLA.
- [ ] Зависимости платного кода допускают проприетарное распространение.
- [ ] Ни одна уже бесплатная функция не попадает в платный слой.
- [ ] Налоговый консультант настроил счета и налоги с продаж; юрист подготовил EULA и договоры.

**Шаги:**
1. ADR: модель редакций, расположение кода, виды лицензий.
2. Опубликовать границу Community и Enterprise в LICENSING.md или на отдельной странице.
3. Создать `ee/` с `ee/LICENSE` или приватный репозиторий. Платные файлы получают
   `SPDX-License-Identifier: LicenseRef-Brokerium-Commercial`, в карте LICENSING.md — новая строка.
4. Собирать Community без `ee/` (например, через Go build tags), Enterprise — с ним. Метка
   Enterprise-образов — `org.opencontainers.image.licenses="LicenseRef-Brokerium-Commercial"`,
   в Helm-чарте — выбор редакции и Secret с ключом.
5. Реализовать проверку ключей и защищённое хранение ключа подписи.
6. Не принимать внешние PR в `ee/`.
7. Опубликовать порядок покупки и страницы для OEM и сервис-провайдеров, обновить FAQ.
8. Объявить по шаблону.

**График:** −3 месяца — юрлицо, юрист, граница редакций; −2 месяца — публикация границы,
каркас `ee/` и ключей; −1 месяц — бета для первых клиентов по оценочной лицензии; T0 —
общедоступный релиз и анонс.

**Нельзя:**
- переносить в платный слой функции, которые уже бесплатны: это обещание GOVERNANCE.md и урок
  MinIO;
- включать в платный код вклады без CLA или код под GPL и AGPL;
- продавать права на сторонние зависимости.

**Шаблон анонса:**
```text
Title: Introducing Brokerium Enterprise

Today we are announcing Brokerium Enterprise, a commercially licensed edition for teams that
need <SSO, advanced roles, audit export>. It will be generally available on <YYYY-MM-DD>.

What this means for Brokerium Community:
- Every feature that is free today stays free; we do not move existing features behind a
  paywall (see GOVERNANCE.md).
- Community releases remain source-available under the PolyForm Shield License 1.0.0 and free
  to use, including in production. The Community/Enterprise boundary is public: <link>.

How Enterprise works: Enterprise features live in <the ee/ directory | a separate repository>
under a commercial license and are activated with an offline license key. Licenses for service
providers and OEM partners are available: <link>. FAQ: <link>. Questions: <link>.

<Name>, <Company>
```

## 6. Сценарий C: смена по требованию инвестора или покупателя

**Типичные требования:**
- перейти на OSI-лицензию ради роста — сценарий A;
- перейти на лицензию, привычную их юристам (FSL, BSL, ELv2), — по сути ослабление, процедура
  как в сценарии A;
- закрыть будущие версии — возможно: опубликованные остаются под Shield, и их форк всё равно не
  может конкурировать. Так поступила Cal.com в 04.2026, публичный репозиторий стал Cal.diy под
  MIT ([блог](https://cal.com/blog/cal-diy-open-source-to-closed-source)). Обещание
  GOVERNANCE.md о бесплатных функциях обсудите до подписания;
- выпустить коммерческую редакцию — сценарий B;
- очистить цепочку прав — аудит CLA, договор уступки IP и регистрация знака
  ([готовность к продаже](sale-readiness.md)).

**Предусловия** — раздел 2, плюс:
- [ ] Требование получено письменно: term sheet, LOI или письмо.
- [ ] Юрист оценил, что из требований выполнимо.
- [ ] Инвестор или покупатель знает об обещаниях в GOVERNANCE.md и LICENSING.md.

**Шаги:**
1. Уточните мотив: риск для их бизнеса, позиционирование, требования их юристов.
2. Вместе с юристом отделите выполнимое от невозможного (раздел 1).
3. Оцените последствия для пользователей, форков, репутации и выручки.
4. Закрепите смену в документах сделки как условие или обязательство после закрытия: с датой,
   ответственным и тем, кто платит за аудит и юристов.
5. Подготовьте ветку по разделу 3, но не мёрджите её до подписания или закрытия. Если сделка
   сорвётся, ничего необратимого не случится.
6. После закрытия пройдите общую процедуру и объявите изменения.

**График:** term sheet → аудит и подготовка ветки во время due diligence → подписание →
закрытие → анонс не позже чем за 30 дней до вступления → релиз под новой лицензией.

**Нельзя:**
- менять лицензию авансом, до подписания документов;
- обещать отзыв лицензий у опубликованных версий или перелицензирование кода без CLA и
  сторонних зависимостей;
- скрывать пробелы CLA: их перечисляют в disclosure schedule к договору.

**Шаблон анонса:**
```text
Title: Brokerium joins <COMPANY>: what changes and what does not

<COMPANY> has <acquired | invested in> Brokerium. [Bulat Valiev continues as maintainer.]

What changes:
- Starting with <vX.(Y+1).0> on <YYYY-MM-DD>, new releases are published by <COMPANY> under
  <NEW TERMS>.
- The repository moves to <github.com/ORG/REPO>; old links redirect automatically.
- [Container images move to <NAMESPACE>; existing tags stay available until <YYYY-MM-DD>.]

What does not change:
- Every published version keeps the license it was released with. Nothing is revoked.
- [Features that are free today stay free.]
- Security reports still go to <link>. FAQ: <link>. Questions: <link>.

<Name>, <Title>, <COMPANY>
```

## 7. Уроки прецедентов

| Кейс | Что произошло | Урок для Brokerium |
| --- | --- | --- |
| Redis и Valkey | 2024: BSD сменили на RSALv2 и SSPL, через 8 дней появился Valkey (Linux Foundation; AWS, Google, Oracle). 2025: Redis добавил AGPL, CEO признал, что смена «hurt our relationship with the Redis community» ([Valkey](https://valkey.io/topics/history/), [Redis](https://redis.io/blog/agplv3/)) | Ужесточение после успеха даёт форк за дни; знак заставил форк взять новое имя |
| HashiCorp и OpenTofu | 2023: MPL сменили на BSL, SDK и API остались под MPL. Форк OpenTofu (Linux Foundation, с 2025 в CNCF) упрекал в «little or no advance notice». 02.2025: IBM купила HashiCorp за $6,4 млрд ([HashiCorp](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license), [IBM](https://newsroom.ibm.com/2025-02-27-ibm-completes-acquisition-of-hashicorp,-creates-comprehensive,-end-to-end-hybrid-cloud-platform)) | Объявлять заранее; смена лицензии не помешала крупной сделке |
| Elastic и OpenSearch | 2021: Apache сменили на ELv2 и SSPL, AWS форкнула 7.10.2 в OpenSearch и цитировала обещание Elastic 2018 года «we never will». 2024: Elastic добавила AGPL ([AWS](https://aws.amazon.com/blogs/opensource/stepping-up-for-a-truly-open-source-elasticsearch/), [Elastic](https://www.elastic.co/blog/elasticsearch-is-open-source-again)) | Не обещать «навсегда» |
| MinIO | 2021: Apache сменили на AGPL. 2025: из бесплатной редакции убрали админ-UI и бинарники. 02.2026: репозиторий в архиве, доверие потеряно, сообщество сделало форк ([репозиторий](https://github.com/minio/minio)) | Не урезать бесплатную версию задним числом |
| Liquibase | 2025: Apache сменили на FSL. Негатив, проекты CNCF вроде Keycloak начали искать замену ([issue](https://github.com/keycloak/keycloak/issues/43391)); пришлось исправлять формулировки «open source» | Ужесточение запускает уход экосистемы; формулировки должны совпадать с лицензией |

Вывод: защитная лицензия выбрана с первого дня, поэтому ужесточать её не придётся. Любое
изменение — ослабление или новая платная редакция, объявленные заранее, без отъёма бесплатного.
