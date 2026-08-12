package du

import "testing"

func TestRun(t *testing.T) {
	if err := Run(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}
