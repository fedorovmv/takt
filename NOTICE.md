# Notice

Проект использует идеи и публично описанные модели поведения Archon: Markdown-команды, DAG-workflows, циклы, approval nodes и hooks. Исходный код Archon в эту реализацию не копировался.

Название продукта — `Takt`. Локальный module path `takt` используется в alpha-архиве и должен быть заменён на адрес будущего репозитория перед публикацией.

## Third-party runtime dependency

Takt uses `go.yaml.in/yaml/v3` v3.0.4 for YAML syntax parsing. The module is maintained by the YAML organization at `github.com/yaml/go-yaml` and is distributed under permissive MIT/Apache-2.0 terms. Its source is not vendored into this archive; Go resolves the exact version and checksum from `go.mod`/`go.sum`.
