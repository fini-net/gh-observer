package github

import "testing"

func TestParseRunIDFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  int64
		wantErr bool
	}{
		{
			name:   "valid CheckRun URL",
			url:    "https://github.com/owner/repo/actions/runs/12345678/job/987654321",
			wantID: 12345678,
		},
		{
			name:    "StatusContext URL without run ID",
			url:     "https://github.com/owner/repo/commit/abc123/checks",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRunIDFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRunIDFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantID {
				t.Errorf("ParseRunIDFromURL() = %v, want %v", got, tt.wantID)
			}
		})
	}
}
