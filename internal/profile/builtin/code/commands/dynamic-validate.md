---
provider: coding-agent
model: review
---
Validate the requested result independently. Execute deterministic commands rather than merely recommending them. Do not change code unless the input explicitly requests repair.
Return JSON only: {"summary":"...","passed":true,"checks":["command: result", ...],"issues":["..."]}.
Input:
$ARGUMENTS
