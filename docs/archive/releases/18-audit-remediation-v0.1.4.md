# Стабилизация классификации родительского `loop_group` в v0.1.4-alpha

## Причина изменения

Повторный аудит выявил, что истечение timeout родительского `loop_group` корректно переводило активный дочерний узел в `timed_out`, но затем контейнер возвращал производную ошибку `loop_group exhausted` с классом `exit`. В результате родительский узел становился `failed`, а исходная причина терялась.

Похожий риск существовал для внешней cancellation: состояние родительского узла сохранялось как `cancelled`, но bare `context.Canceled` на уровне Run мог получить код `internal`.

## Исправление

После возврата действия узла runtime проверяет attempt context до обработки `allow_failure` и производных ошибок контейнера.

При завершённом контексте классификация имеет приоритет:

- deadline attempt → `timed_out`;
- cancellation attempt → `cancelled`.

Это правило применяется ко всем типам узлов, включая `loop_group`.

На уровне Run bare `context.Canceled` и `context.DeadlineExceeded` также нормализуются в `cancelled` и `timed_out`, а не в `internal`.

## Регрессии

Добавлены тесты:

1. timeout родительского `loop_group` во время выполнения child;
2. cancellation родительского `loop_group` во время выполнения child;
3. согласованность статусов и error code родительского Node и Run.

## Итог

После v0.1.4-alpha известный P1 по классификации parent loop закрыт. Следующий этап — fake-assistant contract suite, который должен повторно закрепить timeout/cancel, конкурентный stdout/stderr, malformed result и fresh/resume.
