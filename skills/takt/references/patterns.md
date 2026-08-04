# Рабочие композиции

## 1. Один агентский узел с inline prompt

```yaml
nodes:
  - id: implement
    assistant: pi
    model: deep
    session: fresh
    prompt: |
      Изучи запрос:
      ${input}

      Выполни работу в текущем workspace.
```

## 2. Разные модели в разных узлах

```yaml
nodes:
  - id: draft
    model: fast
    prompt: Подготовь первый вариант для ${input}

  - id: review
    depends_on: [draft]
    model: deep
    prompt: |
      Проверь и исправь результат предыдущего узла.
      Вывод: ${nodes.draft.output}
```

Если оба узла работают с одними файлами, явно укажи агенту, какие файлы читать и изменять. Не полагайся только на текстовый output первого узла.

## 3. Генерация → валидатор → feedback → retry

```yaml
nodes:
  - id: implement
    command: implement
    attempts:
      max: 3
    session: resume
    timeout: 20m
    hooks:
      after_node:
        - id: validate-result
          bash: ./.takt/tools/validate-result
          on_failure:
            action: retry
            session: resume
      before_complete:
        - id: preserve-result
          bash: |
            mkdir -p "$ARTIFACTS_DIR"
            cp result.yaml "$ARTIFACTS_DIR/result.yaml"

  - id: final-validation
    depends_on: [implement]
    bash: ./.takt/tools/validate-result
```

Prompt команды должен содержать `${feedback}`.

## 4. Approval после детерминированной проверки

```yaml
- id: approve
  depends_on: [final-validation]
  approval:
    message: Результат прошёл проверку. Подтвердите публикацию.
    capture_response: true
```

## 5. Cleanup после любого результата

```yaml
- id: cleanup
  depends_on: [implement]
  trigger_rule: all_done
  bash: rm -f temporary.file
  allow_failure: true
```

## 6. Условная ветка

```yaml
- id: publish
  depends_on: [validate]
  when: nodes.validate.exit_code == 0
  bash: ./publish.sh
```

## 7. Структурированный quality validator

В stdout должен быть ровно один envelope:

```json
{
  "protocol_version": "takt-validation/v1alpha1",
  "type": "validation_result",
  "valid": false,
  "score": 60,
  "checks": {
    "syntax": {"passed": true, "score": 100},
    "semantics": {"passed": false, "score": 20}
  },
  "diagnostics": [
    {
      "code": "INVALID_REFERENCE",
      "severity": "error",
      "path": "routes[0].steps[1]",
      "message": "Неизвестный компонент"
    }
  ]
}
```

Логи и предупреждения направляй в stderr. Для невалидного результата допустим exit code 1; Takt сохранит envelope, но успех засчитает только при `completed && valid=true`.
