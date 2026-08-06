---
assistant: coding-agent
model: review
---
Capture the unchanged repository baseline before implementation.

Requirements:
- inspect the repository state and the checks relevant to the stated goal;
- run only safe, non-mutating build/test/validation commands;
- distinguish passing checks, known failures and checks that could not be run;
- do not modify product code, tests, configuration or Git history;
- report exact commands and concise evidence;
- return only the required JSON object.

Task context:
$USER_MESSAGE

Previous structured-output feedback:
${feedback}
