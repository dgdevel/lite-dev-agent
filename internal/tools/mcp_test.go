package tools

import (
	"testing"
)

func TestParseCommandArgs(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"nixdevkit /some/path", []string{"nixdevkit", "/some/path"}},
		{"nixdevkit", []string{"nixdevkit"}},
		{"  cmd  arg1  arg2  ", []string{"cmd", "arg1", "arg2"}},
		{`cmd "arg with spaces" arg2`, []string{"cmd", "arg with spaces", "arg2"}},
		{`cmd 'single quoted' arg2`, []string{"cmd", "single quoted", "arg2"}},
		{"", nil},
	}

	for _, tt := range tests {
		result := parseCommandArgs(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseCommandArgs(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("parseCommandArgs(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], v)
			}
		}
	}
}
