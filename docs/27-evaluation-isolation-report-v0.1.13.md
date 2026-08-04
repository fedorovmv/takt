# Усиление изоляции и отчёта evaluation в v0.1.13-alpha

## Причина изменения

Аудит `v0.1.12-alpha` выявил две возможности повреждения evaluation-набора: разные имена заданий могли нормализоваться в один `case_id`, а output внутри workspace template рекурсивно попадал в копируемое дерево. Отчёт также не сохранял подтверждённый resume и диагностику, по которой выполнялся retry.

## Изоляция заданий

До создания output runner:

1. получает полный список Markdown-заданий;
2. нормализует имена в `case_id`;
3. отклоняет любую коллизию с перечислением обоих файлов;
4. канонизирует `workspace-template` и `output`, включая существующие символические ссылки;
5. отклоняет совпадение и вложенность каталогов в любую сторону.

Таким образом, `--replace` удаляет только workspace одного заранее уникального задания и не может затронуть соседний Run. Копирование template не может рекурсивно захватить output.

## Диагностический отчёт

`NodeState` сохраняет поле `resumed`. Для каждого узла `report.json` теперь содержит:

- `session_id` и `resumed`;
- `exit_code`, `error_code` и `error`;
- накопленный `feedback`;
- `diagnostic_output`;
- attempts, usage и `output_truncated`.

На уровне Run и Summary сохраняется число узлов, завершившихся подтверждённым resume. Route DSL eval assertion требует две попытки, `resumed=true`, наличие `ROUTE_INVALID` в feedback и успешный JSON-ответ полного валидатора в diagnostic output.

## Регрессии

Добавлены проверки:

- `a b.md` и `a+b.md` отклоняются до создания output;
- output внутри workspace template отклоняется до копирования;
- resume, feedback, ошибка и diagnostic output переносятся из `RunState` в отчёт;
- CLI eval suite проверяет диагностические поля на полном Route DSL workflow.
