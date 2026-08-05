# Политики возможностей узлов в v0.1.27-alpha

## Цель среза

Локальный workflow должен задавать ограничения AI-узла как проверяемый контракт, а не как просьбу внутри prompt. Takt теперь вычисляет эффективную политику до запуска assistant, проверяет поддержку adapter, сохраняет применённые ограничения и передаёт их через специализированный adapter или универсальный process protocol.

Server, Web UI и БД не входят в этот срез. Они остаются proposals для будущего нелокального режима.

## Поля workflow

```yaml
- id: classify
  command: classify-change
  allowed_tools: []
  skills: []

- id: review
  command: review-code
  denied_tools: [edit, write]
  skills: [skills/go-review]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
  requires: [tool_policy, skills, mcp]
```

Поддерживаются:

- `allowed_tools` — allowlist; явный `[]` означает отсутствие инструментов;
- `denied_tools` — дополнительный deny-list;
- `skills` — имена или локальные пути; явный `[]` запрещает inherited skills;
- `mcp` — JSON-конфигурация относительно workflow;
- `sandbox.filesystem: read_only`;
- `sandbox.network: deny`;
- `requires` — дополнительные capability names.

Поля разрешены только у `command` и `prompt`. Structural container хранит настройки внутри дочернего workflow. Governed `workflow` принимает `policy`, которая становится верхней границей дочернего Run.

## Наследование

При создании child Run:

- `allowed_tools` и `skills` пересекаются;
- `denied_tools` и `requires` объединяются;
- `read_only` и `network: deny` сохраняются как наиболее строгие значения;
- inherited MCP нельзя заменить другим файлом;
- явный пустой allowlist сохраняет запрет после resume.

Эффективная политика записывается в `NodeState.policy`, inherited policy — в child `RunState`. Локальные skill/MCP resources входят в fingerprint определения.

## Capability negotiation

Стандартные capabilities:

- `tool_policy`;
- `skills`;
- `mcp`;
- `sandbox_filesystem`;
- `sandbox_network`.

Runtime проверяет их до вызова adapter. При отсутствии capability процесс не стартует. Универсальный `process` объявляет возможности в config, потому что их выполняет внешняя программа. Pi/OpenCode публикуют только встроенные возможности; config не может приписать им неподдерживаемую зарезервированную capability.

## Adapter mapping

### Process

Политика передаётся:

- в `takt-assistant/v1alpha1` как `request.policy`;
- в `TAKT_POLICY_JSON`.

### Pi

- tools переводятся в `--tools` или `--no-tools`;
- skills — в `--skill` и `--no-skills`;
- `read_only` исключает bash/edit/write;
- MCP и network deny отклоняются как неподдерживаемые.

### OpenCode

- tool/skill permissions и MCP объединяются с `OPENCODE_CONFIG_CONTENT`;
- path skills читаются Takt и внедряются в prompt как обязательные инструкции;
- `read_only` запрещает edit/bash/task;
- network deny отклоняется как неподдерживаемый.

## Граница безопасности

Текущие filesystem/network поля являются **assistant-enforced policy**, а не системным sandbox. Они защищают от молчаливого игнорирования ограничений и делают поведение воспроизводимым, но не изолируют недоверенный бинарник или произвольный shell. OS sandbox, secret redaction и path/network enforcement остаются отдельным усилением перед многопользовательским режимом.

## Исправления ревью

В тот же релиз вошли:

- канонизация workspace и repository root через `filepath.EvalSymlinks`, включая macOS `/var` → `/private/var`;
- агрегация usage скрытых узлов structural composition;
- запрет `takt cancel` для всех terminal Run, включая `failed`;
- governed recursion validation уже в `takt validate`;
- удаление worktree-ветки только когда она не содержит коммитов поверх base;
- явная документация Run-scoped переключения execution workspace после dynamic gate.

## Проверки

Контракт `scripts/test-policies.sh` проверяет explicit empty allowlists, MCP resolution, persisted policy, capability list и отказ до запуска неподдерживающего adapter. Unit/contract tests также покрывают Pi/OpenCode mapping, child policy inheritance, fingerprint ресурсов и macOS-style symlink paths. `.github/workflows/ci.yml` запускает полный `make check` на `ubuntu-latest` и `macos-latest`, чтобы path-sensitive regressions блокировали релиз в CI.
