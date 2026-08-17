package memory

import "testing"

func TestCloneCopiesNestedPointers(t *testing.T) {
	type nested struct {
		Values []string
	}
	type value struct {
		Nested *nested
	}

	original := value{Nested: &nested{Values: []string{"original"}}}
	copied, err := clone(original)
	if err != nil {
		t.Fatalf("clone() error = %v", err)
	}
	copied.Nested.Values[0] = "changed"

	if got := original.Nested.Values[0]; got != "original" {
		t.Fatalf("clone shares nested storage with original: %q", got)
	}
}
