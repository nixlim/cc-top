package alerts

import (
	"testing"
)

func TestTruncateSessionID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "long ID is truncated",
			input: "sess-1234567890abcdef",
			want:  "sess-1234567...",
		},
		{
			name:  "short ID unchanged",
			input: "sess-123",
			want:  "sess-123",
		},
		{
			name:  "exactly 12 chars unchanged",
			input: "123456789012",
			want:  "123456789012",
		},
		{
			name:  "13 chars truncated",
			input: "1234567890123",
			want:  "123456789012...",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateSessionID(tc.input)
			if got != tc.want {
				t.Errorf("truncateSessionID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
