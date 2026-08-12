package evaluation

import _ "embed"

//go:embed fixtures/fake-gh
var fakeGHFixture []byte

// FakeGHFixture returns the canonical fake GitHub command for test fixtures.
func FakeGHFixture() []byte { return append([]byte(nil), fakeGHFixture...) }
