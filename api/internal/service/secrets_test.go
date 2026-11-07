package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/repository"
)

func TestSecretStoreRoundTripKeepsCiphertextOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	store, err := newSecretStore(path, strings.Repeat("0f", 32))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"gateway": "router-password", "ont": "ont-password"}
	if err = store.save(want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"router-password", "ont-password"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("password was written in clear text")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode = %v", info.Mode().Perm())
	}
	got, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got["gateway"] != want["gateway"] || got["ont"] != want["ont"] {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestSecretStoreRejectsWrongOrInvalidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc")
	good, err := newSecretStore(path, strings.Repeat("0f", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = good.save(map[string]string{"gateway": "secret"}); err != nil {
		t.Fatal(err)
	}
	bad, err := newSecretStore(path, strings.Repeat("1e", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.load(); err == nil {
		t.Fatal("a different key decrypted the file")
	}
	if _, err = newSecretStore(path, "not-hex"); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestSecretsSurviveRestartOnlyWithKey(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r, err := repository.NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Profile{ID: "gateway", Name: "Router", Host: "192.168.1.1", Username: "root", Adapter: "openwrt"}

	// Without a key the engine keeps the old behaviour: nothing reaches disk.
	e, err := New(ctx, r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetConfig(ctx, []domain.ProfileInput{{Profile: p, Password: "first-password"}}, 30); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "secrets.enc")); !os.IsNotExist(err) {
		t.Fatal("credential file written without a key")
	}

	// With a key the password is restored after a restart.
	t.Setenv("SIGNAL_ATLAS_SECRET_KEY", strings.Repeat("0f", 32))
	keyed, err := New(ctx, r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = keyed.SetConfig(ctx, []domain.ProfileInput{{Profile: p, Password: "second-password"}}, 30); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.secrets["gateway"] != "second-password" {
		t.Fatal("password was not restored after restart")
	}

	// Clearing the password must also clear the stored copy.
	if err = restarted.SetConfig(ctx, []domain.ProfileInput{{Profile: p, ClearPassword: true}}, 30); err != nil {
		t.Fatal(err)
	}
	again, err := New(ctx, r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.secrets["gateway"] != "" {
		t.Fatal("a cleared password came back after restart")
	}
}

func TestCredentialFileWithoutKeyIsReported(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r, err := repository.NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIGNAL_ATLAS_SECRET_KEY", strings.Repeat("0f", 32))
	e, err := New(ctx, r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Profile{ID: "gateway", Name: "Router", Host: "192.168.1.1", Username: "root", Adapter: "openwrt"}
	if err = e.SetConfig(ctx, []domain.ProfileInput{{Profile: p, Password: "kept-password"}}, 30); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "secrets.enc")); err != nil {
		t.Fatal("credential file missing after saving with a key")
	}
	// Losing the key must be loud, not a silent empty start.
	os.Unsetenv("SIGNAL_ATLAS_SECRET_KEY")
	if _, err = New(ctx, r, dir, ""); err == nil {
		t.Fatal("missing key was ignored while a credential file exists")
	}
}
