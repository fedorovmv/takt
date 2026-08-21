Implement `mini-du` using this exact CLI contract: positional `PATH...` (default `.`), `-s`, `-k`, uppercase `-H` humanized binary units (`0B`, one decimal below 10, rounded integers from 10), `-h`/`--help`, `--`, combined `-sk`/`-ks`/`-sH`, and fail-closed unknown options. Without `-k`, numeric output is also integer kibibytes, exactly matching `du -k`; `-k` is an explicitly accepted alias, not a different unit. Numeric values use filesystem allocated blocks, matching the host `du -k` oracle, not logical file length. Humanized examples: `0B`, `1024 bytes as 1KiB`, `1536 bytes as 1.5KiB`, `12KiB` without a decimal. This case focuses on multiple paths, Unicode, and spaces.

Help stdout must be exactly:

```text
Usage: mini-du [-s] [-k|-H] [--] [PATH...]
  -s          display only a total for each path
  -k          display sizes in 1024-byte units
  -H          display humanized binary units (KiB, MiB, GiB)
  -h, --help  display this help
```
