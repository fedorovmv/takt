#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./internal/evidence -count=1
go test ./internal/control -run 'TestSegmentControlsTreatUnchangedBaselineFailureAsEvidenceNotRegression|TestScheduleAutomaticRepairParksAfterExactlyOneRepair|TestScheduleAutomaticRepairParksWhenNoCheckBearingBlockExists|TestDynamicCandidateSHAChangesWithWorkspaceContent|TestTaskStatusProjectsParkedPlanAsNeedsInput|TestAttentionIncludesParkedPlanWithFailureCode|TestExternalSideEffectRequiresReconciliationBeforeExpiredClaimReplay' -count=1
go test ./internal/workflow -run TestValidateExternalSideEffectContract -count=1
go test ./internal/taskroute -run TestInferSignalsMatchesLexemesNotSubstrings -count=1

echo "evidence baseline failure routing contract: PASS"
