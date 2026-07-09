package main

import "testing"

func TestParseReleaseArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantDir    string
		wantDryRun bool
	}{
		{"empty", nil, ".", false},
		{"dir only", []string{"pkg"}, "pkg", false},
		// Regression: --dry-run alone must NOT become the directory.
		{"dry-run only", []string{"--dry-run"}, ".", true},
		{"flag then dir", []string{"--dry-run", "pkg"}, "pkg", true},
		{"dir then flag", []string{"pkg", "--dry-run"}, "pkg", true},
		{"first dir wins", []string{"a", "b"}, "a", false},
		{"unknown flag ignored", []string{"-x", "pkg"}, "pkg", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, dryRun := parseReleaseArgs(tt.args)
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if dryRun != tt.wantDryRun {
				t.Errorf("dryRun = %v, want %v", dryRun, tt.wantDryRun)
			}
		})
	}
}
