# Участие в разработке Takt

Спасибо за интерес к Takt. Проект принимает сообщения о дефектах,
документационные исправления и сфокусированные pull request, соответствующие
текущему локальному trusted-runtime scope.

## Перед началом

1. Проверьте существующие issues и [`CHANGELOG.md`](CHANGELOG.md).
2. Для вопроса об использовании сначала изучите
   [`docs/user-guide.md`](docs/user-guide.md) и
   [`docs/12-document-map.md`](docs/12-document-map.md).
3. Для заметного изменения поведения сначала создайте issue с проблемой,
   ожидаемым результатом и предлагаемой границей изменения.
4. Уязвимости не публикуйте в обычном issue; следуйте
   [`SECURITY.md`](SECURITY.md).

Публичный issue tracker:
<https://github.com/fedorovmv/takt/issues>.

## Scope проекта

Takt остаётся компактным локальным orchestration runtime. В текущий scope не
входят Web UI, сетевой или многопользовательский сервер, БД, выполнение
недоверенных workflow, собственный coding-agent tool loop и общий plugin
framework.

Перед архитектурным предложением прочитайте
[`docs/01-project.md`](docs/01-project.md),
[`docs/04-architecture.md`](docs/04-architecture.md) и
[`ARCHITECTURE_DECISIONS.md`](ARCHITECTURE_DECISIONS.md).

## Сообщение о дефекте

Хороший отчёт содержит:

- версию `takt version`, OS и версию Go;
- минимальный Workflow и Config без секретов;
- точную команду запуска;
- ожидаемое и фактическое поведение;
- terminal status, релевантные события и диагностику;
- указание, воспроизводится ли проблема повторно.

Не прикладывайте credentials, приватный исходный код, полный `.takt/` или
неотредактированные provider logs.

## Предложение возможности

Опишите пользовательскую проблему и почему существующие `script`, `command`,
`prompt`, adapter или композиция Workflow её не решают. Новое YAML-поле
допустимо только когда runtime должен видеть его для governance,
наблюдаемости, resumability или auditability.

Предложения о server/untrusted режиме требуют отдельной threat model, sandbox
и политики секретов.

## Локальная разработка

Требования:

- Go 1.23+;
- Git;
- Node.js 22 и TypeScript 5.7.2 для обязательного host-integration smoke в CI.

Базовая проверка checkout:

```bash
make check
```

Команда форматирует Go-код, запускает `go vet`, компилирует все пакеты,
выполняет быстрые contract suites, собирает CLI и проверяет TypeScript
integration. Без установленного `tsc` локальный smoke явно пропускается; CI
требует pinned TypeScript compiler.

Полный технический контур и структура репозитория описаны в
[`DEVELOPMENT.md`](DEVELOPMENT.md).

## Pull request

Pull request должен:

- решать одну сформулированную проблему без параллельного расширения scope;
- переиспользовать существующие use cases, scheduler и registries;
- содержать минимальную проверку, которая воспроизводит изменяемое поведение;
- обновлять документацию и schema при изменении публичного контракта;
- не включать generated state, `.takt/`, credentials и unrelated formatting;
- проходить `make check`.

Для изменения runtime, persistence, protocol или публичной schema выполните
также релевантные contract/E2E проверки и по возможности полный gate:

```bash
make check-full
./scripts/verify.sh
```

В описании PR укажите проблему, решение, границы, выполненные команды и
известные ограничения. Live Pi/OpenCode smoke и benchmark отделяйте от
детерминированных release checks.

## Документация

Пишите для конкретной аудитории:

- README — краткая точка входа;
- `docs/README.md` — индекс каталога документации;
- `docs/user-guide.md` — установка, настройка и использование;
- `docs/03-specification.md` и schemas — нормативный внешний контракт;
- `docs/04-architecture.md` и ADR — устройство и границы;
- `docs/05-implementation-status.md`, roadmap и changelog — состояние и
  история.

Не переносите release history и backlog обратно в README. Примеры команд
должны соответствовать фактическому CLI, а YAML snippets — текущему dialect.

Документационная правка без изменения поведения не требует новой версии
продукта или искусственного product test; проверьте ссылки, snippets и
затронутые documentation contracts.

Основной язык пользовательской и contributor-документации — русский. Имена
CLI, API, файлов, схем и точные protocol terms сохраняйте на английском.
`SECURITY.md` и `NOTICE.md` намеренно остаются на английском как документы для
международного security/legal контекста.

## Версионирование и changelog

Правила версионирования описаны в [`AGENTS.md`](AGENTS.md). Изменение
пользовательского поведения или публичного контракта может потребовать новой
alpha-версии. Чистая документация и внутренний refactoring версию продукта не
повышают.

Пользовательские изменения добавляйте в `Unreleased` файла
[`CHANGELOG.md`](CHANGELOG.md). Исторические записи не переписывайте без
необходимости исправить фактическую ошибку.

## Общение и лицензия

Во всех пространствах проекта действует
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). Обсуждайте идеи уважительно и по
существу, критикуйте решение и явно отделяйте факт от предположения.

Отправляя contribution, вы соглашаетесь на его распространение по лицензии
проекта [MIT](LICENSE).
