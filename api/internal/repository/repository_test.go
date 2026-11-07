package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFilePersistenceAndBoundaries(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var got map[string]int
	if !errors.Is(r.Read(ctx, "samples", &got), ErrNotFound) {
		t.Fatal("missing document should be distinguishable")
	}
	if err = r.Write(ctx, "samples", map[string]int{"count": 10}); err != nil {
		t.Fatal(err)
	}
	second, _ := NewFile(dir)
	if err = second.Read(ctx, "samples", &got); err != nil || got["count"] != 10 {
		t.Fatalf("persistence failed %v %v", got, err)
	}
	st, _ := os.Stat(filepath.Join(dir, "samples.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("local files must be private")
	}
	for _, key := range []string{"../outside", "/tmp/escape", "a/b", "", "SAMPLES"} {
		if r.Write(ctx, key, got) == nil {
			t.Fatalf("accepted unsafe key %q", key)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(r.Write(cancelCtx, "samples", got), context.Canceled) {
		t.Fatal("canceled write accepted")
	}
}
