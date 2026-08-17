Implement `mini-du` with this exact public contract:

- `PATH...` are positional paths; with no path use `.`.
- `-s` prints only the total for each supplied path.
- `-k` prints integer kibibytes (1024-byte units).
- Without `-k`, numeric output is also integer kibibytes, exactly matching `du -k`; `-k` is an explicitly accepted alias, not a different unit.
- `-H` prints humanized allocated size using binary units: `0B`, one decimal below 10, and rounded integers from 10 (`KiB`, `MiB`, `GiB`).
- Humanized examples: `0B`, `1024 bytes as 1KiB`, `1536 bytes as 1.5KiB`, `12KiB` without a decimal.
- `-h` and `--help` print the exact usage/help text and exit 0 without scanning paths.
- `--` ends options so paths beginning with `-` are accepted.
- Combined short flags `-sk`, `-ks`, and `-sH` are accepted; unknown options such as `-z` exit 1 and write a diagnostic to stderr.

Preserve normal `du` behavior for directory traversal, symlinks, hardlinks, missing paths, Unicode, and spaces.

Help stdout must be exactly:

```text
Usage: mini-du [-s] [-k|-H] [--] [PATH...]
  -s          display only a total for each path
  -k          display sizes in 1024-byte units
  -H          display humanized binary units (KiB, MiB, GiB)
  -h, --help  display this help
```
