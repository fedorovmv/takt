# Результаты проверки Takt v0.1.21-alpha

Проверено 5 августа 2026 года.

## Новый профильный контракт

- `takt init code` устанавливает встроенный пакет в `.takt/profiles/code/`;
- `.takt/config.yaml` создаётся только при отсутствии пользовательской конфигурации;
- `takt validate code` и `takt run code` разрешают workflow/config по имени профиля;
- Markdown input сохраняет абсолютный путь и исходное содержимое;
- runtime не создаёт обязательный task AST;
- повторная установка без `--force` отклоняется;
- authoring skill 0.3.0 описывает работу с готовыми профилями.

## Пройденные проверки

```text
go test ./...
go test -race ./...
go vet ./...
make check
./scripts/verify.sh

fake-assistant contract suite: PASS
Pi adapter contract suite: PASS
OpenCode adapter contract suite: PASS
Route DSL end-to-end: PASS
Route DSL evaluation: PASS
Route DSL evaluation isolation: PASS
Takt authoring skill: PASS
code profile contract: PASS
documentation check: PASS
```

## Внешние smoke-тесты

Реальные Pi и OpenCode smoke в среде сборки не запускались: пользовательские бинарники, credentials и provider-конфигурация отсутствуют. Контрактные fake adapters прошли.
