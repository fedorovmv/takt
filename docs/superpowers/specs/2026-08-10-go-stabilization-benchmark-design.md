# Go-first benchmark: дизайн доказательной стабилизации

## Статус и цель

Дизайн подтверждён пользователем 2026-08-10. Цель — получить первые воспроизводимые live-свидетельства качества Go user journey на Pi и OpenCode с Qwen-моделями, а каждый обнаруженный дефект Takt превращать в regression test и минимальный root-cause fix. После сравнительного прогона текущим OpenCode default выбран Qwen3-Coder-Next со штатными tool calls; Qwen 3.6 сохранён как диагностический evidence.

Срез не меняет runtime заранее. Существующий `takt eval benchmark` уже предоставляет repeat, fingerprints, pairwise comparison, usage, time-to-valid и fail-closed quality contract; новый runner не нужен.

## Выбранный подход

Добавляется один компактный production-shaped corpus из пяти изолированных Go-задач. Каждая задача воспроизводит класс ошибок из фактических контрактов Takt, но работает в небольшом standalone-модуле на стандартной библиотеке:

1. сохранение пользовательских аргументов после `--` без попадания служебных CLI flags в goal;
2. разбор OpenCode NDJSON с дедупликацией `step_finish` usage и приоритетом `error` event;
3. exact resume: запрошенный Session ID обязан совпасть с возвращённым, fresh fallback запрещён;
4. приоритет `timed_out`/`cancelled` над output overflow;
5. обязательный возврат ошибки persistence вызывающему коду.

Corpus помечается `production-shaped`, а не production: реальных обезличенных пользовательских задач нет. Его назначение — первый управляемый evidence slice, а не доказательство репрезентативности всех Go-проектов.

Не выбираются:

- копия всего репозитория Takt как workspace template: она медленнее, хрупче и не нужна для первого сигнала;
- новый benchmark framework или новые public evaluation fields;
- task-level Dynamic Takt в первом срезе: он смешал бы качество Router/Planner с качеством реализации;
- архитектурный refactor по размеру файлов без воспроизведённого дефекта.

## Структура benchmark

Каталог `examples/go-benchmark` повторно использует существующий формат Route DSL benchmark:

- `cases/` — пять Markdown-задач с явным целевым package;
- `cases.yaml` — category, difficulty и `source: production-shaped`;
- `workspace/` — standalone Go module с одной изолированной исходной ошибкой на package и детерминированными тестами;
- `validator/` — внешний Go-валидатор на стандартной библиотеке;
- `strategies/` — direct и feedback-repair workflow;
- Pi/OpenCode config и matrix definitions;
- один `run.sh` для сборки валидатора и запуска выбранного host либо обоих hosts.

Live outputs по умолчанию пишутся в `${TMPDIR:-/tmp}/takt-go-benchmark/evals`, вне Git-репозитория. Это не даёт OpenCode подняться от скопированного workspace к родительскому Git-root и изменить исходный template. `TAKT_BENCH_OUTPUT` сохраняется как override, но для OpenCode также должен указывать вне репозитория; отчёты и session artifacts не входят в manifest или release archive.

## Стратегии и модели

`baseline-direct` выполняет один fresh assistant turn, затем штатную проверку.

`feedback-repair` выполняет тот же prompt, но допускает до трёх попыток. После каждой попытки тот же детерминированный валидатор запускается как `after_node` hook; diagnostics передаются в resumed session. Финальный quality-node повторяет валидатор независимо от текста агента.

Обе стратегии используют логическое имя модели `go-model`:

- Pi: provider `aihub`, model `Qwen/Qwen3.6-27B`;
- OpenCode: прямой provider `aihub-sbt`, model `Qwen/Qwen3-Coder-Next`, agent `build`, explicit `auto_approve: true` только для доверенного benchmark workspace. Qwen 3.6 direct/proxy остаётся сохранённым диагностическим evidence, но не текущим benchmark default из-за нестабильного tool protocol.

OpenCode запускается с собственным флагом `--pure`, поэтому пользовательские plugins, включая `oh-my-opencode-slim`, не участвуют в benchmark. Оба agent-node задают явный `skills: []`; существующая Takt policy передаёт в OpenCode запрет skill tool. Таким образом, agent использует только штатный tool loop OpenCode под политикой Takt, без внешних plugins/skills. Глобальная пользовательская конфигурация OpenCode не изменяется и не копируется в репозиторий.

Отчёт фиксирует requested/resolved model по фактически доступным событиям. Для OpenCode нельзя заявлять наблюдаемость provider-side routing, если NDJSON сообщает только запрошенную CLI model.

Pi и OpenCode запускаются отдельными matrix reports с одинаковыми corpus, repeat и workflow. Это позволяет сравнивать direct против repair внутри каждого host без смешения host-различий с эффектом стратегии.

## Валидатор и поток данных

Каждый case содержит машинно-читаемую строку с допустимым package. Workflow перед проверкой сохраняет точный исходный case во временный файл workspace. Внешний validator:

1. разбирает package только из allowlist пяти cases;
2. сравнивает workspace с исходным template и разрешает изменения только в production `.go` целевого package;
3. отклоняет изменения `go.mod`, `*_test.go`, соседних packages и benchmark metadata;
4. проверяет `gofmt`;
5. запускает для целевого package `go test -count=1`, `go test -race -count=1` и `go vet`;
6. печатает ровно один `takt-validation/v1alpha1` envelope.

Validator сохраняет stdout/stderr команд в diagnostics, но success определяется только `quality_node_status=completed && valid=true`. Невалидный результат возвращает предметный envelope и ненулевой exit code; malformed envelope является infrastructure failure существующего evaluation contract.

Validator собирается вне копируемого workspace и передаётся workflow через environment. Его source path и workspace template входят в benchmark fingerprint; coding-agent не редактирует исполняемый validator.

## Порядок live-прогона

1. Собрать `bin/takt` и validator.
2. Проверить corpus локально: каждый исходный package должен падать своим тестом, а минимальная эталонная правка — проходить validator.
3. Запустить Pi и OpenCode smoke с `repeat=1`.
4. Если measurement path корректен, запустить обе matrix с `repeat=3`.
5. Зафиксировать success@1, final success, attempts-to-valid, time, tokens, cost, unstable cases и execution identity.
6. Для каждого воспроизводимого дефекта Takt сначала добавить regression test в фактическую общую точку, затем внести минимальный fix и повторить затронутый case.

Model failure или невалидная реализация являются benchmark outcome и не останавливают остальные cases. Подготовка workspace, недоступный host, повреждённый quality envelope или ошибка записи отчёта являются infrastructure failure и останавливают соответствующую matrix.

OpenCode smoke дополнительно проверяет фактический argv с `--pure`, отсутствие загрузки внешних plugins в host log и запрет skill tool из Takt policy. Недоступность provider endpoint не считается результатом качества модели.

## Неизвестные и ограничители

**Проектный шлюз: READY.** Открытых P0 нет.

- P1 «нет реального корпуса» закрыт обратимым production-shaped corpus с честной маркировкой. После появления обезличенных задач synthetic evidence не смешивается с production evidence.
- P1 «correctness или cost» закрыт выбором correctness как первого gate; cost/time сначала только измеряются. Порог стоимости добавляется после первого полного baseline.
- P2 «следующий домен» отложен до результатов Go; Document и Route DSL не входят в этот срез.
- P2 strict host conformance не связан с benchmark: успешные assistant runs не повышают bundled host-control integrations из `guarded`.

## Критерии готовности

Срез завершён, когда:

1. пять cases и validator имеют детерминированную локальную self-check;
2. direct и feedback-repair используют один corpus и один quality contract;
3. smoke `repeat=1` пройден на Pi и OpenCode либо дал сохранённый infrastructure defect;
4. полный `repeat=3` выполнен для обоих hosts либо конкретный внешний blocker явно зафиксирован;
5. каждый исправленный дефект имеет Go regression test до production change;
6. `make check` и `scripts/verify.sh` проходят после изменений;
7. отчёт не содержит credentials, provider secrets или session artifacts;
8. `docs/05-implementation-status.md`, changelog и `docs/archive/verification/TEST_RESULTS-v0.1.57-2026-08-18.md` отражают только фактически полученное evidence.

## Условия остановки

Вернуть задачу на проектирование, если реализация потребует нового public Workflow/Config/evaluation поля, изменения durable runtime semantics, ослабления validation gate или отдельного scheduler/executor. Локальный воспроизводимый дефект существующего контракта исправляется через TDD без расширения scope.
