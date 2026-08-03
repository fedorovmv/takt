# Ключевые архитектурные решения

## ADR-001. Новый Go-runtime вместо порта Archon

Archon используется как поведенческая спецификация. Исходный TypeScript runtime не переносится построчно.

## ADR-002. Кодовый агент остаётся внешним исполнителем

Takt не реализует tool-calling, файловые инструменты, MCP, LSP и память агента.

## ADR-003. Модель отделена от исполнителя

Workflow выбирает логическую модель и assistant независимо. Один assistant может запускаться с разными моделями.

## ADR-004. Command — Markdown-инструкция

Термин `command` означает переиспользуемый prompt-файл, а детерминированная команда называется `bash`.

## ADR-005. Hooks делятся на portable и native

Portable hooks исполняются runtime. Native hooks передаются конкретному adapter и не интерпретируются ядром.

## ADR-006. Approval — сохраняемое состояние

Ожидание человека не блокирует stdin. Run сохраняется и продолжается отдельной командой.

## ADR-007. Локальное файловое хранилище в прототипе

`state.json` и `events.jsonl` позволяют проверить модель без сервера и базы данных. Production-store будет заменяемым.

## ADR-008. Текущий scope — локальный trusted runtime

До отдельной threat model Takt не принимает workflow и команды от недоверенных пользователей и не используется как многопользовательский сервер.

## ADR-009. Failure узла не останавливает DAG немедленно

Node failure является terminal-результатом узла. Scheduler продолжает доступные ветви, включая `all_done`, и вычисляет итог Run после завершения графа.

## ADR-010. `allow_failure` разрешает только exit code

Ошибки запуска, timeout, cancellation, protocol и internal errors не могут быть скрыты через `allow_failure`.

## ADR-011. Root DAG и loop DAG используют одну семантику

`depends_on`, `when`, `trigger_rule`, hooks, attempts и ошибки реализуются общим scheduler.

## ADR-012. Persistence использует revision consistency

State и event одного перехода получают одинаковую revision. Рассогласование считается ошибкой хранилища, а не восстанавливается молча.

## ADR-013. Поддерживается документированный YAML subset

До появления требования полной YAML 1.2 Takt сохраняет stdlib-only parser, формально ограничивает subset и покрывает block scalar тестами.

## ADR-014. Timeout ограничивает всю попытку узла

`node.timeout` охватывает portable hooks и действие узла. Timeout/cancellation внутри hook сохраняют execution kind и не преобразуются в `hook_failed`.

## ADR-015. Nested loop groups запрещены в v1alpha1

До введения path-based namespace дочерних состояний вложенные `loop_group` отклоняются валидатором и runtime. Это исключает коллизии ID и повреждение состояния внешнего DAG.

## ADR-016. `until` требует успешное завершение проверочного узла

Условие цикла оценивается только для child node со статусом `completed`. Значения output/exit code из `skipped` или failure-like состояний не могут завершить цикл.
