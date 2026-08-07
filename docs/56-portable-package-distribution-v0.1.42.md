# Portable Package Distribution — v0.1.42-alpha

`v0.1.42-alpha` превращает существующий `BlockPackage` из явно подключаемого локального каталога в переносимую поставку. Формат workflow/runtime не меняется: установленный пакет добавляется в тот же доверенный каталог блоков и исполняется тем же scheduler.

## Пользовательский путь

```bash
takt package install ./corp-code --scope project
takt package install git+ssh://git.corp/takt/platform-code.git --scope corporate --ref v2.3.1
takt package list
takt package update platform-code --scope corporate --ref v2.4.0
takt package doctor
takt package sync
```

После установки `block_packages` вручную указывать не требуется. `profile.Resolve` автоматически подключает пакеты из lock-файлов.

## Области и приоритет

Поддерживаются три устанавливаемые области:

```text
global      ~/.takt/packages/global
corporate   <workspace>/.takt/packages/corporate
project     <workspace>/.takt/packages/project
```

При совпадении имени блока действует приоритет:

```text
project > corporate > global > builtin
```

Приоритет выбирает реализацию блока, но governance всех подключённых пакетов объединяется fail-closed. Более локальный пакет не может ослабить корпоративную security policy или лимиты.

## Источники и lock

Поддерживаются `local` и `git` sources. Git-install фиксирует фактический commit. Состояние хранится в:

```text
<workspace>/.takt/takt.lock.json
~/.takt/takt.lock.json               # global
```

Lock содержит имя/версию/scope, source/ref/commit, SHA-256 полного содержимого пакета и сведения о проверенной подписи. Перед подключением locked package к профилю Takt повторно проверяет package integrity/policy; изменённый установленный файл не может попасть в Run до `doctor/sync`. `package sync` восстанавливает Git source по зафиксированному commit. Для local source sync разрешён только если источник всё ещё воспроизводит зафиксированные version/checksum; тихая подмена содержимого запрещена.

## Зависимости и совместимость

`BlockPackage` может объявить:

```yaml
dependencies:
  - name: platform-base
    version: ^2.1.0

requirements:
  takt: ">=0.1.42"
  adapters:
    - name: scm
      domain: scm
      operations: [change.create, change.get]
      reconcile: [change.create]
      level: required
    - name: ci
      domain: ci
      operations: [run.get]
      level: preferred
```

Поддерживаемый version-constraint subset: exact `x.y.z`, `>=x.y.z`, `^x.y.z` и `*`. Установка и обновление проверяют уже установленный dependency graph и не позволяют удалить/обновить пакет так, чтобы зависимость перестала удовлетворяться. Автоматическое скачивание зависимостей в этом релизе не выполняется: зависимый пакет устанавливается явно, после чего операция повторяется.

`takt package doctor` проверяет целостность/совместимость пакетов и тем же capability preflight проверяет adapter requirements. Required adapter requirements проверяются до запуска workflow. Preferred requirement передаётся Router/Planner как недоступная возможность и не блокирует план автоматически. Reconcile capability проверяется только для операций, явно перечисленных в `requirements.adapters[].reconcile` или использующих `side_effect.mode: reconcile`.

## Source policy и подписи

Опциональные политики находятся в `.takt/package-policy.yaml` для project/corporate и `~/.takt/package-policy.yaml` для global. Обе применяются fail-closed.

```yaml
apiVersion: takt/v1alpha1
kind: PackagePolicy
allowed_sources:
  - git:ssh://git.corp/takt/
  - local:/opt/takt/packages
require_signature_scopes: [corporate]
trusted_keys:
  release-2026: <base64-ed25519-public-key>
```

Команда `takt package sign <dir> --key-id ... --key ...` создаёт `package.sig`. Подпись Ed25519 покрывает SHA-256 дерева пакета; `.git` и сам `package.sig` из digest исключены. Проверенная подпись и key id фиксируются в lock.

## Исправления Adapter Platform v0.1.41

- `sdk/agentadapter` теперь содержит публичные request/result/envelope типы и validators, включая `attempt >= 1`, обязательные идентификаторы и ограничения policy;
- conformance fixtures находятся в `sdk/agentadapter/testdata/v1alpha2`; `cmd/takt-fake-assistant` сам использует public SDK validators для v1alpha2 request/result, а его process contract дополнительно проверяет captured stdout тем же `ValidateTranscript`;
- transcript conformance явно не проверяет OS process exit code: соответствие process exit status и `result.exit_code` остаётся обязанностью host/process contract test;
- `adapter doctor` выполняет дополнительную диагностику configured mappings/reconcile capabilities, а `list|describe|doctor` имеют CLI-тесты;
- расширено покрытие `sdk/agentadapter` и `sdk/domainadapter`;
- исправлена формулировка reconcile preflight: он обязателен только для `side_effect.mode: reconcile`, а не для любой mutating operation;
- добавлены регрессии cancel/pause adapter-node, process `max_output_bytes`, BOUNDARY_VIOLATION через DenyReason и финальный evidence verdict.

## Переносимые ресурсы

Package manager копирует весь каталог поставки. Обновление проходит через проверенный staging и переключение lock; при ошибке записи lock прежний каталог восстанавливается. Workflow внутри установленного пакета разрешает соседние `commands/`, `scripts`, path-skills и MCP JSON относительно собственного package tree; эти ресурсы уже входят в transitive content fingerprint `BlockPackage`. Рабочий пример находится в `examples/portable-package/`.

## Границы релиза

Пакетный registry/server не добавлен: для локального trusted runtime достаточно local/Git sources. Production GitHub/GitLab/корпоративные adapters остаются отдельными пакетами поверх `sdk/domainadapter`. Multi-repo orchestration остаётся следующим крупным продуктовым срезом.
