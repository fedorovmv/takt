# Runtime Reliability & Local Security — v0.1.44-alpha

`v0.1.44-alpha` усиливает существующий локальный runtime без второго scheduler, отдельного security-runtime или серверной модели. Срез закрывает раннее завершение fan-out, durable retry/backoff, нормализованные diagnostics, защиту секретов при persistence, фактический OS sandbox для локальных детерминированных процессов и структурированный путь узла. Одновременно исправлены замечания ревью `v0.1.43-alpha`.

## Durable retry и backoff

`attempts` поддерживает ограниченную политику ожидания:

```yaml
attempts:
  max: 4
  retry_on: [exit, timed_out]
  backoff:
    initial: 1s
    multiplier: 2
    max: 15s
    jitter: true
```

Retry остаётся явным: Takt не повторяет произвольную ошибку автоматически. `retry_on` принимает только проверяемые execution kinds; `cancelled` и `external_state_unknown` не становятся обычным retry. Для неизвестного внешнего side effect продолжает действовать reconciliation из `v0.1.40/v0.1.41`.

Перед следующей попыткой runtime вычисляет точный `not_before` и сохраняет его в `NodeState.retry` вместе с delay, kind и fingerprint ошибки. Поэтому выбранный jitter и оставшееся ожидание не пересчитываются после загрузки state. Во время ожидания Takt продолжает проверять pause/cancel. События `node.retry.scheduled` и `node.retry.ready` делают задержку наблюдаемой.

## Нормализованные diagnostics

Execution failure получает `NodeState.diagnostic`:

```json
{
  "code": "exit",
  "kind": "exit",
  "op": "bash",
  "message": "...",
  "fingerprint": "sha256:...",
  "retryable": true
}
```

Fingerprint строится из code/kind/op и нормализованного сообщения. Control/execution workspace заменяются стабильным маркером, а volatile PID/port/attempt/line и длинные случайные числа нормализуются. Каждая `ExecutionState` сохраняет diagnostic своей попытки; успешное завершение очищает текущий `NodeState.diagnostic`, но история attempts остаётся.

Это не semantic similarity и не LLM-классификация. Одинаковый воспроизводимый сбой получает одинаковый fingerprint; новый сбой — другой. Формат предназначен для retry, диагностики, будущего benchmark и связи с failure routing.

## Раннее завершение governed fan-out

`workflow.fan_out.join` теперь имеет фактическую short-circuit семантику:

- `one_success` завершается после первого успешного child Run;
- `all_success` прекращает оставшуюся работу после первого failure-like результата;
- `all_done` по-прежнему ждёт всех children.

Уже запущенные siblings получают cancellation через отдельный batch context; ещё не запущенные children помечаются `cancelled` без запуска. Parent `ChildRunItemState.cancel_reason` и aggregate output используют `fanout_result_decided`, чтобы такую отмену можно было отличить от пользовательского `takt cancel`.

## SecretRef и redaction

Takt не становится хранилищем секретов. Значение берётся из окружения только в момент исполнения:

```yaml
env:
  TOKEN: secret://CORP_TOKEN
```

`secret://NAME` поддерживается в process assistant, process domain adapter и `script.env`. Отсутствующий explicit secret завершается fail-closed. Явный SecretRef регистрируется для redaction независимо от длины значения; эвристическое auto-detection обычных environment variables сохраняет минимальный порог, чтобы короткие строки не превращались в глобальные ложные замены.

Перед записью runtime создаёт отдельную persistence-копию Run state и редактирует известные секреты в:

- Run/node outputs, stdout/stderr, errors, feedback и diagnostics;
- execution history;
- external tool inputs/results;
- persisted events;
- approval/waiting text;
- textual typed artifacts.

Текстовый artifact сохраняется уже с `<redacted>`. Известный секрет в non-text artifact блокирует persistence вместо повреждения бинарного содержимого. Секрет существует в памяти исполняющего процесса и может присутствовать во внутреннем transient live state до завершения текущего вызова; durable Store/event stream остаются редактированными. Публичный control API и прямой CLI после foreground execution повторно загружают Run из Store и не возвращают live state. Для воспроизводимого resume нужно использовать SecretRef, а не передавать секрет как обычный task input.

## Локальный OS sandbox

Assistant policy и OS enforcement теперь различаются явно.

Для `command/prompt` прежний `sandbox.filesystem/network` остаётся capability-контрактом coding-agent adapter. `v0.1.44` не выдаёт его за системную изоляцию.

Для `bash/script` можно запросить реальную локальную изоляцию:

```yaml
sandbox:
  enforcement: required
  filesystem: read_only
  network: deny
```

Поддерживаемые backend:

- Linux — `bubblewrap` (`bwrap`);
- macOS — `sandbox-exec`, если он доступен на конкретной системе.

`required` при отсутствии backend завершает узел до запуска payload. `optional` запускает обычный процесс, но сохраняет `NodeState.sandbox.status: degraded` и причину. `runtime: validation` проходит через тот же wrapper и не является обходным путём. Hooks используют sandbox своего node. В этой версии filesystem contract ограничен `read_only`; произвольные write allowlists и контейнерная изоляция не вводятся.

Локальный runtime по-прежнему однопользовательский и считает workflow/config/packages доверенными. OS sandbox уменьшает blast radius выполняемого процесса, но не создаёт многопользовательскую security boundary.

## Структурированный путь узла

Legacy Node ID остаётся совместимым ключом state и шаблонов. Дополнительно `NodeState.path` и node events получают канонический путь:

```text
build                    -> /build
batch__001__append       -> /batch[1]/append
outer__002__inner__003   -> /outer[2]/inner[3]
```

Это отделяет устойчивую идентичность вложенной композиции от исторического способа кодирования ID через `__` и готовит диагностику/evidence к более глубоким loop/foreach/subworkflow без смены runtime.

## Исправления ревью v0.1.43

- auto-discovery Git repositories канонизирует обе стороны `git rev-parse --show-toplevel` через `EvalSymlinks`; `/tmp -> /private/tmp` и аналогичные macOS-пути больше не дают молча пустой каталог;
- governed repository child test сравнивает физические пути;
- `scripts/test-multi-repo.sh` выбирает `python3` и использует `python` только как fallback;
- merge order является настоящей детерминированной topological sort dependency graph, а не порядком фаз;
- explicit Workspace с пустым `repositories` отклоняется так же, как требует schema;
- Git source allowlist требует URL/path boundary и не принимает `allowed-prefix-evil`;
- `adapter doctor` имеет отрицательный CLI regression и возвращает ошибку при capability mismatch;
- добавлены прямые regressions для multi-repo integrity deny, replanner repository payload, repository fingerprint drift, dependency-results TaskBrief и node-level `workflow.repository` правил;
- retry родительского governed workflow переиспользует уже `completed` child Run. Новый child создаётся только когда предыдущий не завершился успешно; это предотвращает повторную mutating работу в уже завершённом repository.

GitHub Actions release gate выполняется на `ubuntu-latest` и `macos-latest`.

## Release contract

`scripts/test-runtime-reliability-security.sh` фиксирует основные свойства среза: durable backoff/fingerprint, SecretRef/redaction, binary artifact fail-closed, validation sandbox, fan-out short-circuit, reuse completed child, canonical NodePath и regressions ревью `v0.1.43`.
