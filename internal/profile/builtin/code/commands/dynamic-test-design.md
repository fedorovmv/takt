---
assistant: coding-agent
model: implementation
---
Design and add independent tests for the requested behavior before relying on the implementation result.

Requirements:
- derive tests from the task goal, public behavior and investigation evidence;
- modify test files and narrowly required fixtures only;
- avoid changing product implementation to make a test pass;
- record assumptions that could not be established from the task or repository;
- run the focused tests when possible;
- return only the required JSON object.

Task context:
$USER_MESSAGE

Previous structured-output feedback:
${feedback}
