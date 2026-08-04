# Результаты проверки v0.1.18-alpha

## Takt authoring skill

Проверено:

- canonical skill находится в `skills/takt/SKILL.md`;
- frontmatter содержит имя и назначение скилла;
- основной файл фиксирует порядок работы, источники истины и ограничения `takt/v1alpha1`;
- отдельные references описывают config, workflow, рабочие композиции и диагностику;
- скилл различает inline `prompt` и Markdown-команду;
- документирован приоритет model/assistant: узел → frontmatter → defaults;
- запрещено предлагать неподдерживаемые `system_prompt`, nested `loop_group`, approval внутри loop и тихий resume fallback;
- стартовый профиль содержит две модели, явную модель узла, retry/feedback, resume, artifacts и approval;
- `scripts/test-takt-skill.sh` включён в `make check` и `scripts/verify.sh`.

## Проверка шаблонов

Реальным бинарником `takt v0.1.18-alpha` успешно проверены:

```text
skills/takt/assets/validated-agent-profile/.takt/workflows/basic.yaml
skills/takt/assets/validated-agent-profile/.takt/workflows/validated.yaml
```

Оба workflow прошли `takt validate --json` с приложенным config и workspace. Значения provider/model `replace-me` предназначены только для структурной проверки и должны быть заменены перед реальным запуском.

## Регрессии

Повторно проверено:

- unit tests и race detector;
- process и Pi contract suites;
- timeout/cancel/output overflow;
- Pi `agent_settled`, fresh/resume и usage delta;
- Route DSL feedback, retry/resume, artifacts и approval;
- evaluation isolation, fingerprints и предметные метрики;
- validation envelope только из stdout и отдельный stderr;
- схемы и защита документации от отката.

## Команды

```text
gofmt -w cmd internal
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/takt
go build ./cmd/takt-fake-assistant
go build ./cmd/takt-fake-pi
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/test-takt-skill.sh
./scripts/check-docs.sh
./scripts/verify.sh
```

`make check` и `scripts/verify.sh` прошли на рабочем дереве. `MANIFEST.sha256` и полный `scripts/verify.sh` также прошли из чистой распаковки релизного архива.

Реальный Pi smoke и предметный Route DSL запуск для `v0.1.18-alpha` в среде сборки не выполнялись: отсутствуют Pi, пользовательская авторизация, модель и штатный валидатор.
