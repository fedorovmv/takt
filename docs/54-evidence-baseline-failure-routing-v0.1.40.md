# Evidence, baseline и failure routing — v0.1.40-alpha

## Назначение

`v0.1.40-alpha` завершает следующий слой Simple Reliable Development после Role/Brief: Takt хранит доказательства отдельно от текста worker-а, отличает новые регрессии от исходных проблем, связывает итоговую проверку с конкретным содержимым candidate workspace и сохраняет безопасный путь продолжения при материальной остановке.

Пользовательский сценарий не меняется:

```text
/takt <задача>
```

`EvidenceManifest`, fingerprints, failure codes и reconciliation внешних эффектов являются внутренними контрактами Takt и доступны через `task status|explain` и operator API для диагностики.

## EvidenceManifest

Dynamic Plan хранит `EvidenceManifest` рядом с durable plan state. Он содержит:

- baseline до изменения;
- fingerprints известных исходных failures;
- check-to-evidence mapping;
- candidate SHA-256;
- итоговый verdict `pass|fail|partial|stale`.

Идентификатор evidence item формируется из trusted block и check. Повторная проверка того же check после automatic repair заменяет старый результат свежим, поэтому final verdict относится к последнему доказательству, а не к первой неудачной попытке.

`candidate_sha` — это не Git commit SHA. Для managed execution workspace Takt вычисляет content fingerprint `sha256:<hex>` из:

1. базового Git commit;
2. полного `git diff --binary <base> --`;
3. отсортированных untracked paths и SHA-256 их содержимого.

Если содержимое workspace меняется после verdict, старый verdict становится `stale` до новой проверки.

Машиночитаемая форма: `schemas/evidence-manifest.schema.json`.

## Baseline provenance

Блок `baseline` выполняется до mutating phases, когда Task Router включил соответствующий control. Его structured output сохраняет:

- `base_ref`;
- успешные проверки;
- известные failures;
- недоступные проверки;
- evidence.

Каждый известный failure нормализуется и получает стабильный SHA-256 fingerprint. При дальнейшей deterministic validation Takt сравнивает новые issues с baseline:

```text
та же проблема была до изменения
→ BASELINE_FAILURE
→ evidence сохраняется
→ automatic repair не запускается

новая проблема
→ IMPLEMENTATION_FAILURE / VERIFICATION_FAILURE
→ обычный repair/failure routing
```

Сопоставление намеренно консервативно: это exact normalized fingerprint, а не LLM similarity. Неясное совпадение считается новой проблемой.

## Failure routing и parking

Материальная остановка Dynamic Plan теперь может быть `parked`, а не безликим `waiting`/`failed`. Parking record содержит:

- стабильный `code`;
- понятное сообщение;
- владельца решения;
- `retryable`;
- `safe_next_action`;
- список `unsafe_to_repeat`;
- время остановки.

Основной набор кодов:

```text
SPEC_GAP
CONTEXT_INSUFFICIENT
IMPLEMENTATION_FAILURE
VERIFICATION_FAILURE
BASELINE_FAILURE
BOUNDARY_VIOLATION
ENVIRONMENT_FAILURE
SECURITY_HALT
BUDGET_EXCEEDED
EXTERNAL_STATE_UNKNOWN
OWNER_DECISION_REQUIRED
```

`parked` проецируется как `needs_input` и попадает в общую attention queue. Обычный `continue` не снимает parking. Пользователь может дать steering с безопасным решением либо остановить задачу. После steering Takt перепланирует только незавершённый хвост и очищает прежний parking record после принятия нового плана.

Automatic repair использует parking в двух случаях:

- один repair уже исчерпан и required check снова падает;
- безопасный repair невозможно построить, например нет implement/recheck block.

Это предотвращает бесконечные циклы и одновременно не превращает первую техническую ошибку в вопрос пользователю.

## Reconciliation внешних side effects

Для `executor: external` добавлен необязательный контракт:

```yaml
side_effect:
  mode: reconcile
  idempotency_key: optional-stable-key
```

Режимы:

- `idempotent` — повтор после потери worker допустим по обычной retry semantics;
- `reconcile` — после истёкшего claim Takt запрещает новый claim, пока внешний факт не сверён.

Worker/operator выполняет `takt.node.reconcile` с одним из исходов:

```text
not_applied
  → side effect доказанно не произошёл
  → node возвращается в pending и допускает новый claim

applied + receipt + result
  → side effect уже произошёл
  → Takt принимает receipt/result через обычный submit path
  → node завершается без повторного внешнего действия

unknown
  → Run остаётся waiting/external_reconcile
  → повтор запрещён
```

`idempotency_key` сохраняется в durable external execution state. Если пользователь не указал ключ, Takt создаёт устойчивый ключ из `run_id:node_id`.

Это ещё не SCM/tracker/CI SDK. Механизм задаёт безопасную runtime-границу, на которую следующий Adapter SDK сможет опираться для create/comment/transition/merge/publish операций.

## MCP surfaces

Добавлен worker tool `takt.node.reconcile`. Полная совместимая поверхность теперь содержит 54 операции:

```text
agent      5
host       7
worker    13
operator  29
all       54
```

Agent surface остаётся прежней — пять `takt.task.*`. Новая низкоуровневая операция основной LLM не показывается.

## Постоянные регрессии из ревью

В релизный test suite добавлены отдельные тесты на ранее замеченные пробелы:

- token matching `auth != author`, `bug != debug`;
- обе terminal-ветки bounded automatic repair;
- pause re-check перед новой retry-attempt;
- реальный `Waiting.Kind=question` для `capture_response`;
- ошибки очистки operator markers и release advance lock;
- transient recursive summary при linked-but-not-published child;
- plan-fork fingerprint;
- межпроцессный notification dispatch lock;
- bounded notification inbox;
- desktop sink timeout;
- `task start --file` как единственный путь чтения файла;
- default MCP surface при пустом значении;
- capability preflight прямого `takt run` до создания Run;
- persisted `RouterError` и propagation `context.Canceled`;
- baseline classification, candidate fingerprint/stale verdict и external reconciliation.

Старые OpenCode timeout-тесты больше не используют искусственно тесный двухсекундный предел под race-нагрузкой. Provider-timeout fixture остаётся гарантированно дольше тестового deadline. Полный `go test -race ./internal/...` является релизным гейтом этого среза.

## Ограничения

- Candidate fingerprint является SHA-256 содержимого рабочей дельты, а не подписанным supply-chain attestation.
- Baseline failure matching точное; семантически одинаковые, но текстово разные ошибки считаются новыми.
- `parked` реализован для Dynamic Plan. Run-level operator pause/abandon сохраняют собственную семантику и не переименовываются в parking.
- Reconciliation не умеет сам читать GitHub/GitLab/трекер/CI. Внешний adapter обязан предоставить проверенный факт и receipt.
- Exactly-once для внешних систем не заявляется. `reconcile` предотвращает слепой повтор при неизвестном исходе.
