#!/usr/bin/env bash
set -euo pipefail

# Focused release contract for v0.1.44. These tests are deliberately named so
# regressions in durable retry, local security enforcement, fan-out early exit,
# node-path identity, and the v0.1.43 review fixes remain visible.
go test ./internal/diagnostic ./internal/redact ./internal/localsandbox -count=1
go test ./internal/runtime -run 'TestRetryBackoffPersistsDeadlineAndDiagnosticFingerprint|TestSecretRefIsRedactedFromDurableStateEventsAndTextArtifact|TestKnownSecretCannotBePersistedInBinaryArtifact|TestValidationScriptCannotBypassRequiredOSSandbox|TestCanonicalNodePathUsesStructuredNamespace|TestGovernedChildFanOutOneSuccessCancelsUnneededChildren|TestGovernedChildFanOutAllSuccessCancelsAfterFirstFailure|TestGovernedChildRetryReusesCompletedChildRun' -count=1
go test ./internal/workflow -run 'TestValidateRetryBackoffAndTimeoutRetryKind|TestValidateOSSandboxEnforcementOnlyForDeterministicLocalNodes|TestValidateRepositoryChildRunRules' -count=1
go test ./internal/workspacecatalog -run 'TestDiscoveryResolvesSymlinkedWorkspacePath|TestManifestRejectsEmptyRepositories' -count=1
go test ./internal/packagedist -run 'TestGitSourceAllowlistUsesRepositoryBoundary|TestSourceAllowlistUsesPathBoundary|TestSignaturePolicyNegativeCases' -count=1
go test ./internal/application -run 'TestSegmentControlsDenyUndeclaredMultiRepoWorkspaceChange|TestReplannerPayloadContainsRepositoriesAndExecutions|TestRepositoriesForRecordRejectsFingerprintDrift|TestForegroundStartReturnsDurableRedactedState' -count=1
go test ./internal/dynamicplan -run 'TestRepositoryTaskBriefIncludesDependencyResults|TestRepositoryMergeOrderUsesDependenciesNotPhaseOrder' -count=1
go test ./cmd/takt -run 'TestAdapterDoctorReturnsErrorForCapabilityMismatch' -count=1

echo 'Runtime Reliability & Local Security: PASS'
