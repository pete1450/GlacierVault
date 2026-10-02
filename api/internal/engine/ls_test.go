package engine

import "testing"

// TestParseLsLongLine uses real `rustic ls --long` output (rustic 0.11.4).
func TestParseLsLongLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantPath  string
		wantName  string
		wantSize  int64
		wantMtime string // RFC3339 UTC
		wantType  string
		wantOK    bool
	}{
		{
			name:      "file",
			line:      `-rw-rw----        ?        ?    685573 23 May 2024 18:31 "pictures/Immich/thumbs/35528420-9109-40fc-b823-0f7331a1f5bf/67/a2/67a2df7d-0360-4037-bf1d-78780f2a9eb1-preview.jpeg"`,
			wantPath:  "pictures/Immich/thumbs/35528420-9109-40fc-b823-0f7331a1f5bf/67/a2/67a2df7d-0360-4037-bf1d-78780f2a9eb1-preview.jpeg",
			wantName:  "67a2df7d-0360-4037-bf1d-78780f2a9eb1-preview.jpeg",
			wantSize:  685573,
			wantMtime: "2024-05-23T18:31:00Z",
			wantType:  "file",
			wantOK:    true,
		},
		{
			name:      "dir",
			line:      `drwxrwxrwx        ?        ?         0  6 Jan 2025 18:26 "pictures/Immich/thumbs/35528420-9109-40fc-b823-0f7331a1f5bf/67/a3"`,
			wantPath:  "pictures/Immich/thumbs/35528420-9109-40fc-b823-0f7331a1f5bf/67/a3",
			wantName:  "a3",
			wantSize:  0,
			wantMtime: "2025-01-06T18:26:00Z",
			wantType:  "dir",
			wantOK:    true,
		},
		{
			name:      "path with spaces",
			line:      `-rw-rw----        ?        ?     15290 24 May 2024 00:09 "pictures/mov desktop/PICT0038.JPG"`,
			wantPath:  "pictures/mov desktop/PICT0038.JPG",
			wantName:  "PICT0038.JPG",
			wantSize:  15290,
			wantMtime: "2024-05-24T00:09:00Z",
			wantType:  "file",
			wantOK:    true,
		},
		{
			name:   "blank line rejected",
			line:   "",
			wantOK: false,
		},
		{
			name:   "info log line rejected",
			line:   `[INFO] reading index...: 49 done in 266.60ms`,
			wantOK: false,
		},
		{
			name:   "truncated line rejected",
			line:   `-rw-rw----        ?        ?    685573 23 May 2024`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLsLongLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.Path != tt.wantPath || got.Name != tt.wantName || got.Size != tt.wantSize ||
				got.Mtime != tt.wantMtime || got.Type != tt.wantType {
				t.Errorf("got %+v, want path=%q name=%q size=%d mtime=%q type=%q",
					got, tt.wantPath, tt.wantName, tt.wantSize, tt.wantMtime, tt.wantType)
			}
		})
	}
}
