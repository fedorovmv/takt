# Security scope

## Supported trust model

Takt `v0.1.3-alpha` is a local, single-user, trusted runtime.

Trusted inputs:

- workflow and config files;
- Markdown commands;
- shell scripts and hook commands;
- assistant argv/env configuration;
- workspace contents;
- Run ID received from the local CLI after validation.

The current version must not be exposed as a service that accepts these values from untrusted users.

## Current protections

- strict unknown-field validation;
- safe Run ID format;
- per-Run lock for `answer` and `resume`;
- definition fingerprints before resume;
- timeout and output limit for process assistants;
- Unix process-group termination on context cancellation;
- revision consistency between state and event log.

These controls improve reliability but do not form a sandbox.

## Unsupported scenarios

- multi-user server;
- arbitrary workflows from external users;
- execution in a shared privileged workspace;
- secret-bearing stdout/stderr without external redaction;
- network isolation;
- filesystem isolation;
- protection from malicious shell commands or coding agents.

## Secrets

Takt does not intentionally copy assistant environment variables into state or events. However, command output, hook feedback, model responses and error messages may contain secrets and are currently persisted without automatic redaction.

Before production-like use, define:

- secret sources;
- redaction patterns and structured secret markers;
- fields prohibited in state/events;
- retention and deletion policy;
- tests for stdout/stderr and error-message leakage.

## Server/untrusted prerequisites

A future server or untrusted mode requires at minimum:

- sandboxed process execution;
- workspace and artifact path policy;
- network egress policy;
- secret broker and redaction;
- authentication and authorization;
- durable distributed locking;
- quotas and resource limits;
- audit retention policy;
- recovery from stale locks and interrupted commits.
