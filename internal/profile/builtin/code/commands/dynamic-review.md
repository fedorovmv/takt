---
provider: coding-agent
model: review
---
Review or adversarially verify the requested scope. Inspect actual diffs and evidence, focus on correctness and regressions, and do not modify code.
Return JSON only: {"summary":"...","approved":true,"findings":["..."],"evidence":["..."]}.
Input:
$ARGUMENTS
