package command

import "testing"

func TestParse(t *testing.T) {
	src := `---
description: Test
provider: demo
model: large
argument-hint: <request>
---
Hello $ARGUMENTS
`
	c, err := Parse("x", "x.md", src)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "demo" || c.Assistant != "demo" || c.Model != "large" || c.ArgumentHint != "<request>" || c.Body != "Hello $ARGUMENTS" {
		t.Fatalf("unexpected: %+v", c)
	}
}
