# Corporate trusted block package

This example adds corporate blocks and governance to Dynamic Takt without changing the runtime.
Copy the directory into the project, then explicitly trust it from the installed profile:

```yaml
block_packages:
  - workflows/blocks/package.yaml
  - ../../../packages/corporate-engineering/package.yaml
```

Validate and inspect it:

```bash
takt block validate .takt/packages/corporate-engineering/package.yaml
takt block list --profile code
takt block describe corp-validate --profile code
```

The package can carry research and implementation templates, mandatory checks, branch rules,
a change-request template, allowed integrations, node policy, and hard planning limits. Dynamic
Takt records the catalog fingerprint in every plan and refuses execution after the package changes.
