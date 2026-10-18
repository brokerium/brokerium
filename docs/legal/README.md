# Юридические плейбуки Brokerium / Legal playbooks

> **Abstract (English).** Owner-facing playbooks for Brokerium (Brokerium for Apache Kafka),
> written in Russian: the licensing strategy behind the PolyForm Shield License 1.0.0, a
> step-by-step relicensing playbook and a sale-readiness checklist. They are not legal advice:
> an IP lawyer must review the CLA, the license strategy and the deal documents before the first
> commercial contract, investment or sale.

> [!WARNING]
> **Это не юридическая консультация.** Плейбуки составлены по публичным источникам на 15.10.2026
> и описывают решения владельца проекта. Перед первым коммерческим договором, привлечением
> инвестиций или продажей IP-юрист должен проверить CLA и CCLA, стратегию лицензирования и
> документы сделки. Тексты CLA и CCLA версии 1.0 юрист ещё не проверял.

Перед первым коммерческим договором, инвестициями или продажей:

- [ ] IP-юрист проверил [CLA](../../CLA.md) и [CCLA](../../CCLA.md).
- [ ] IP-юрист проверил стратегию лицензирования и строки в [LICENSE](../../LICENSE).
- [ ] IP-юрист проверил документы конкретного договора или сделки.

## Плейбуки

| Документ | О чём | Когда открывать |
| --- | --- | --- |
| [Стратегия лицензирования](licensing-strategy.md) | Почему PolyForm Shield 1.0.0, что разрешено и запрещено, сравнение лицензий, политика зависимостей, сигналы для пересмотра | Вопрос о лицензии от пользователя, выбор зависимости, ежегодный пересмотр |
| [Плейбук смены лицензии](relicensing-playbook.md) | Три сценария: открыть код шире, выпустить коммерческую редакцию, смена по требованию инвестора или покупателя; шаблоны анонсов | Сработал сигнал из стратегии, пришёл term sheet или запрос OEM |
| [Готовность к продаже](sale-readiness.md) | Что подготовить заранее, этапы сделки, data room, красные флаги, кого привлечь | Уже сейчас для подготовки и при первом контакте с покупателем |

## Связанные файлы

- [LICENSE](../../LICENSE) — текст PolyForm Shield 1.0.0 со строками `Required Notice` и
  `Licensor Line of Business`.
- [LICENSING.md](../../LICENSING.md) — лицензия простыми словами, FAQ и карта лицензий.
- [CLA.md](../../CLA.md) и [CCLA.md](../../CCLA.md) — соглашения с авторами вкладов и компаниями.
- [TRADEMARKS.md](../../TRADEMARKS.md) — правила использования имени Brokerium.
- [GOVERNANCE.md](../../GOVERNANCE.md) — кто принимает решения, политика лицензий,
  преемственность.
- [CONTRIBUTING.md](../../CONTRIBUTING.md), [SECURITY.md](../../SECURITY.md) и
  [NOTICE](../../NOTICE).

## Текущее положение на 18.10.2026

- Лицензиар и владелец всех прав — Bulat Valiev как частное лицо; юрлица пока нет. План:
  сначала OSVČ, позже s.r.o. ([ADR 0014](../adr/0014-name-protection-and-business-setup.md)).
- Код лежит в репозитории `brokerium/brokerium` организации `brokerium` на GitHub (владелец
  организации — `bvaliev`), образы — в личном Docker Hub (`bvaliev/brokerium-server`,
  `bvaliev/brokerium-agent`).
- Домен `brokerium.kaufen` куплен на имя владельца, DNS в Cloudflare.
- Подписи CLA собирает CLA Assistant Lite в ветку `cla-signatures` того же репозитория.
- Имя Brokerium используется как товарный знак, но не зарегистрировано; заявка в EUIPO
  запланирована на начало 2027 года.

## Как поддерживать плейбуки

1. Пересматривайте их раз в год и перед крупными событиями: v1.0.0, первый коммерческий
   договор, инвестиции, переговоры о продаже.
2. Каждое решение по лицензии записывайте в ADR в `docs/adr/`.
3. После консультации с юристом отмечайте в начале документа дату проверки и что изменилось.
