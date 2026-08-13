# Flow evaluation

Create a suite with:

```bash
takt eval flow init code:feature-development --output evals/feature
```

Add `config.yaml`, an executable `validator`, and a complete initial repository
under `cases/<id>/workspace/`. A case has `input.md`, `expected.yaml`, and its
workspace. The suite's validator receives a versioned request on stdin and emits
one `takt-validation/v1alpha1` result on stdout. Its deterministic result—not
the assistant response—decides correctness.

Run a case with:

```bash
takt eval flow evals/feature/suite.yaml --case example --repeat 1 --output evals/out --trace
```

For the bundled mini-du live corpus, use `make eval-smoke`, `make eval-feature`,
`make eval-review`, or `make eval-architect` from the repository root.
The live flow targets fail after five minutes without assistant progress. Set
`EVAL_IDLE_TIMEOUT=10m` before `make` only when the provider legitimately needs
a longer silent interval; tool and assistant message events reset the timer.
Pi streaming activity also resets it without printing every partial token;
`node.active` shows the current idle duration, limit, last activity and wait.

Inspect `report.json` and `cases/<id>/repeat-001/` evidence before changing a
workflow or validator. Do not treat fake SCM fixtures as a remote provider or
security boundary. Trace progress is printed to stderr; stdout stays valid JSON.
