# Состояние реализации

## Реализовано

- YAML/JSON loader со строгой проверкой неизвестных полей;
- статическая проверка ID, ссылок, DAG и запрет вложенных `loop_group` в `v1alpha1`;
- модельный каталог;
- `mock` и универсальный `process` assistants;
- Markdown commands;
- узлы `command`, `prompt`, `bash`, `approval`, `loop_group`;
- `when` и `trigger_rule` в корневом DAG и внутри `loop_group`;
- hooks и retry с feedback;
- pause/resume approval;
- fingerprints workflow/config/commands;
- блокировка Run, ревизии state/events и проверка согласованности;
- классификация execution errors: `exit`, `start`, `timed_out`, `cancelled`, `protocol`, `internal`;
- статусы Node `completed`, `failed`, `errored`, `timed_out`, `cancelled`, `skipped`, `blocked`;
- `allow_failure` только для ненулевого exit code;
- timeout попытки узла, включая portable hooks и дочерний DAG `loop_group`;
- общий thread-safe лимит stdout/stderr process assistant;
- JSON success/error envelope CLI;
- JSONL events и файловые artifacts;
- CLI `validate`, `run`, `resume`, `answer`, `status`, `command run`;
- JSON Schemas текущего `v1alpha1`;
- unit-, race-, CLI- и сквозные тесты стабилизационного контура.

## Осознанно упрощено

- последовательное выполнение готовых узлов DAG;
- файловое локальное состояние;
- ограниченный язык выражений;
- документированный YAML subset;
- вложенные `loop_group` запрещены вместо namespace дочернего состояния;
- отсутствие server/Web UI/MCP/worktree.

## Не считается production-ready

Перед промышленным применением нужны:

- песочница процессов;
- ограничения файловой системы и сети;
- политика секретов и redaction;
- проверка блокировок и cancellation на целевых ОС;
- миграции схемы;
- полноценное восстановление по журналу;
- наблюдаемость, бюджеты и лимиты стоимости;
- серверная модель конкуренции и авторизации, если появится remote scope.

## Ближайший целевой срез

Главный следующий этап — fake-assistant contract suite по `10-assistant-adapter-spec.md`. Он должен закрепить:

- prompt через argv/stdin;
- параметры модели и рабочий каталог;
- concurrent stdout/stderr under output limit;
- timeout/cancel обычного узла, hooks и родительского `loop_group`;
- fresh/resume;
- malformed assistant result;
- корректную классификацию start/exit/protocol errors.

После прохождения suite можно начинать специализированный Pi или OpenCode adapter и Route DSL end-to-end.

## Известные расхождения с целевой семантикой v0.2

- нет `takt cancel`;
- capability negotiation декларативна и не проверяется;
- session resume не проверен на реальном агенте;
- дочернее состояние `loop_group` не имеет полноценного namespace, поэтому nested loops запрещены;
- шаблоны сохраняют неизвестные переменные вместо строгой ошибки;
- события не содержат schema version, attempt и iteration как отдельные обязательные поля;
- structured output и schema validation пока отсутствуют.
