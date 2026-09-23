package notify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glaciervault/api/internal/db"
)

func TestValidateURL(t *testing.T) {
	valid := []string{
		"discord://webhook_id/webhook_token",
		"mailto://user:pass@gmail.com",
		"ntfy://ntfy.sh/mytopic",
		"slack://tokenA/tokenB/tokenC/channel",
		"tgram://bottoken/chatid",
		"json://localhost:8080/hook",
	}
	for _, u := range valid {
		if err := ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", u, err)
		}
	}
	invalid := []string{
		"",
		"   ",
		"not-a-url",
		"://missing-scheme",
		"-t evil",
		"--body=x",
		"discord://has space/x",
	}
	for _, u := range invalid {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want error", u)
		}
	}
}

func openTestDB(t *testing.T) *Manager {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database)
}

func TestConfigRoundTrip(t *testing.T) {
	m := openTestDB(t)
	ctx := context.Background()

	// Defaults: empty destinations, all switches off.
	cfg, err := m.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Destinations) != 0 || cfg.NotifyBackupCompleted || cfg.NotifyWarmupCompleted || cfg.NotifyRestoreCompleted {
		t.Fatalf("default config = %+v, want all zero", cfg)
	}

	want := Config{
		Destinations:           []string{"discord://webhook_id/token", "ntfy://ntfy.sh/topic"},
		NotifyBackupCompleted:  true,
		NotifyWarmupCompleted:  false,
		NotifyRestoreCompleted: true,
	}
	if err := m.SaveConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := m.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Destinations) != 2 || got.Destinations[0] != want.Destinations[0] || got.Destinations[1] != want.Destinations[1] {
		t.Errorf("destinations = %v, want %v", got.Destinations, want.Destinations)
	}
	if !got.NotifyBackupCompleted || got.NotifyWarmupCompleted || !got.NotifyRestoreCompleted {
		t.Errorf("switches = %+v, want %+v", got, want)
	}

	// Blank lines are dropped; invalid URLs are rejected.
	if err := m.SaveConfig(ctx, Config{Destinations: []string{"", "  ", "bogus"}}); err == nil {
		t.Error("SaveConfig with invalid URL = nil, want error")
	}
	if err := m.SaveConfig(ctx, Config{Destinations: []string{"  ntfy://x/y  ", ""}}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.GetConfig(ctx)
	if len(got.Destinations) != 1 || got.Destinations[0] != "ntfy://x/y" {
		t.Errorf("destinations after cleanup = %v", got.Destinations)
	}
}

// fakeApprise writes a shell script masquerading as the apprise CLI: it
// records its argv to a file and exits 0 (or 1 when told to fail).
func fakeApprise(t *testing.T, fail bool) (bin, record string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "apprise")
	record = filepath.Join(dir, "argv.txt")
	script := "#!/bin/sh\necho \"$@\" > " + record + "\n"
	if fail {
		script += "echo simulated failure >&2\nexit 1\n"
	} else {
		script += "exit 0\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin, record
}

func TestDispatchArgv(t *testing.T) {
	m := openTestDB(t)
	m.appriseBin, _ = fakeApprise(t, false)
	urls := []string{"discord://a/b", "ntfy://x/y"}
	if err := m.dispatch(context.Background(), "Title here", "Body here", urls); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(m.appriseBin), "argv.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	// URLs must be separate argv entries, in order, after -t/-b.
	want := "-t Title here -b Body here discord://a/b ntfy://x/y\n"
	if got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestDispatchFailure(t *testing.T) {
	m := openTestDB(t)
	m.appriseBin, _ = fakeApprise(t, true)
	err := m.dispatch(context.Background(), "T", "B", []string{"ntfy://x/y"})
	if err == nil {
		t.Fatal("dispatch against failing apprise = nil, want error")
	}
}

func TestDispatchNoBinary(t *testing.T) {
	m := openTestDB(t)
	m.appriseBin = ""
	if err := m.dispatch(context.Background(), "T", "B", []string{"ntfy://x/y"}); err == nil {
		t.Error("dispatch with no apprise binary = nil, want error")
	}
}
