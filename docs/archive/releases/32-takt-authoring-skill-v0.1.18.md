# Скилл создания и настройки Takt — v0.1.18-alpha

## Цель

В релиз добавлен переносимый скилл `skills/takt/`, который помогает кодовому агенту создавать и изменять пользовательские Takt-профили. Он ориентирован на практический результат: config, workflow, Markdown-команды, проверяемые hooks, запуск CLI и диагностику ошибок.

Скилл дополняет корневой `AGENTS.md`, но решает другую задачу:

- `AGENTS.md` регулирует изменение исходного кода самого Takt;
- `skills/takt/SKILL.md` помогает использовать Takt в прикладном проекте.

## Структура

```text
skills/takt/
  SKILL.md
  references/
    configuration.md
    workflows.md
    patterns.md
    troubleshooting.md
  assets/
    validated-agent-profile/
```

Основной файл содержит обязательный алгоритм работы и критичные инварианты. Подробности вынесены в references, чтобы агент загружал только нужный материал.

## Что умеет скилл

- выбирать между inline `prompt` и Markdown-командой;
- задавать assistant/model в defaults, команде или конкретном узле;
- собирать зависимости, условия и `trigger_rule`;
- оформлять validator → feedback → retry/resume;
- добавлять approval и артефакты;
- использовать `loop_group` в пределах `v1alpha1`;
- проверять workflow через `takt validate --json`;
- отличать структурно проверенный профиль от реально проверенной внешней интеграции.

## Стартовый профиль

`assets/validated-agent-profile` содержит два workflow:

1. `basic.yaml` — inline prompt и явная модель узла;
2. `validated.yaml` — Markdown-команда, детерминированный validator, retry с resume, артефакт и approval.

Config использует безопасные placeholders `replace-me`. Они позволяют проверить структуру, но должны быть заменены на существующие provider/model перед реальным запуском.

## Проверка

```bash
make skill
```

Скрипт `scripts/test-takt-skill.sh` проверяет обязательные файлы и выполняет `takt validate` для обоих шаблонных workflow. Проверка включена в `make check` и `scripts/verify.sh`.

## Совместимость

Скилл описывает текущий контракт `takt/v1alpha1` версии `v0.1.18-alpha`. При изменении внешних полей, приоритетов или runtime-семантики references и шаблон должны обновляться вместе со спецификацией и схемами.
