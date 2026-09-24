package flag

import (
	"testing"
)

func TestTruthy(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", false},
		{"0", false},
		{"", false},
	}
	for _, c := range cases {
		t.Setenv("FLAG_TEST_TRUTHY", c.value)
		if got := Truthy("FLAG_TEST_TRUTHY"); got != c.want {
			t.Fatalf("Truthy(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}
