package command

import "testing"

func TestParse(t *testing.T) {
	src := `---
description: Test
assistant: demo
model: large
---
Hello $USER_MESSAGE
`
	c, err := Parse("x", "x.md", src)
	if err != nil {
		t.Fatal(err)
	}
	if c.Assistant != "demo" || c.Model != "large" || c.Body != "Hello $USER_MESSAGE" {
		t.Fatalf("unexpected: %+v", c)
	}
}
