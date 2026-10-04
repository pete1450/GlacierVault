package scheduler

import (
	"testing"
	"time"
)

func TestScheduleLocation(t *testing.T) {
	// TZ set to a valid IANA name wins.
	t.Setenv("TZ", "America/Chicago")
	if loc := scheduleLocation(); loc.String() != "America/Chicago" {
		t.Errorf("TZ=America/Chicago -> %q", loc.String())
	}
	s := New(nil, nil, nil, nil)
	if s.Location().String() != "America/Chicago" {
		t.Errorf("scheduler location = %q", s.Location().String())
	}
	// A 2 AM entry must resolve to 2 AM Chicago wall-clock time.
	entryTime := time.Date(2026, 10, 5, 2, 0, 0, 0, s.Location())
	if entryTime.Format("15:04 MST") != "02:00 CDT" {
		t.Errorf("2 AM in scheduler zone = %q", entryTime.Format("15:04 MST"))
	}

	// Invalid TZ falls back to the container local zone (no panic).
	t.Setenv("TZ", "Not/AZone")
	if got := scheduleLocation(); got != time.Local {
		t.Errorf("invalid TZ -> %q, want time.Local", got.String())
	}

	// Unset TZ falls back to the container local zone.
	t.Setenv("TZ", "")
	if got := scheduleLocation(); got != time.Local {
		t.Errorf("unset TZ -> %q, want time.Local", got.String())
	}
}
