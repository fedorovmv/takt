# Пакеты профилей и встроенный профиль code — v0.1.21-alpha

## Решение

Takt получает пользовательский слой готовых профилей поверх существующего runtime. Профиль связывает workflow, config и правила подготовки входа, но не создаёт отдельную модель исполнения.

Первый встроенный профиль `code` работает напрямую с Markdown-планом. Исходный файл остаётся авторитетным документом: Takt передаёт coding agent абсолютный путь и содержимое плана, а агент обновляет существующие отметки выполнения после реализации и проверки. Обязательный task AST, JSON или YAML не создаётся.

## Команды

```bash
takt init code
takt validate code --workspace . --json
takt run code --workspace . --input docs/plan.md --json
```

`takt init code` устанавливает пакет в `.takt/profiles/code/` и создаёт `.takt/config.yaml`, если конфигурации ещё нет. После установки workflow можно запускать по имени профиля вместо пути.

## Состав профиля

- `profile.yaml` — manifest пакета;
- `workflow.yaml` — реализация плана, проверка, ревью и approval;
- `commands/implement-plan.md` — реализация Markdown-плана;
- `commands/review-changes.md` — независимое ревью;
- `tools/validate` — детерминированная проверка проекта;
- `config.example.yaml` — стартовая конфигурация Pi/OpenCode.

## Проверка проекта

`tools/validate` использует `TAKT_VALIDATE_COMMAND`, затем последовательно ищет `scripts/verify.sh`, `make check`, `go test ./...` или `npm test`. Профиль не скрывает выбранную команду и позволяет проекту заменить её без изменения runtime.

## Расширение входов

Manifest содержит ограниченный `input` contract. В v0.1.21 реализован Markdown с сохранением пути. Другие источники — issue, OpenSpec, JSON/YAML — должны добавляться как адаптеры подготовки входа. Они не должны становиться обязательным промежуточным представлением для Markdown-плана.

## Следующий runtime-срез

- переиспользуемые `subworkflow`;
- последовательный `foreach` над явно выбранным input adapter;
- профильные пакеты `review`, `route-dsl`, `document` и `migration`;
- Git worktree и task transaction как типизированные действия.
