# Профиль совместимости с Archon

Проект не заявляет полную совместимость с Archon. Цель — сохранить знакомые концепции и облегчить перенос процессов.

## Близкие конструкции

| Archon | Takt |
|---|---|
| `.archon/commands/*.md` | `.takt/commands/*.md` |
| `command` / `prompt` | `command` / `prompt` |
| `bash` | `bash` |
| DAG `nodes` | DAG `nodes` |
| `depends_on` | `depends_on` |
| `when` | ограниченный `when` |
| `trigger_rule` | три базовых правила |
| `loop_group` | `loop_group` |
| approval nodes | `approval` |
| provider/model | assistant/model |
| SDK hooks | portable runtime hooks + future native pass-through |

## Отличия

- собственная `apiVersion`;
- другой формат model/assistant registry;
- hooks в первой версии выполняются главным образом снаружи агентной сессии;
- workflow state локальный;
- нет server/Web UI/platform adapters;
- нет встроенных workflow Archon.

## Принцип переноса

Workflow Archon сначала преобразуется в профиль `takt/v1alpha1`, после чего `takt validate` сообщает о неподдерживаемых полях и capabilities.
