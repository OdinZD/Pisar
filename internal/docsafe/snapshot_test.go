package docsafe

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshot_Same(t *testing.T) {
	base := Snapshot{Size: 3, ModTime: time.Unix(1000, 0), SHA256: "aa"}
	tests := []struct {
		name string
		o    Snapshot
		want bool
	}{
		{"identical", base, true},
		{"only mtime differs", Snapshot{Size: 3, ModTime: time.Unix(2000, 0), SHA256: "aa"}, true},
		{"hash differs", Snapshot{Size: 3, ModTime: time.Unix(1000, 0), SHA256: "bb"}, false},
		{"size differs", Snapshot{Size: 4, ModTime: time.Unix(1000, 0), SHA256: "aa"}, false},
		{"zero value", Snapshot{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.Same(tt.o); got != tt.want {
				t.Errorf("Same = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTakeSnapshot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.docx")
	write(t, file, "abc", 0o644)
	mtime := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.docx")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		path    string
		want    Snapshot
		wantErr error // matched with errors.Is; nil with wantAny false means success
		wantAny bool  // any error will do
	}{
		{
			name: "file",
			ctx:  context.Background(),
			path: file,
			// sha256("abc"), the FIPS 180 test vector
			want: Snapshot{Size: 3, ModTime: mtime, SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		},
		{name: "missing", ctx: context.Background(), path: filepath.Join(dir, "none"), wantErr: fs.ErrNotExist},
		{name: "folder", ctx: context.Background(), path: dir, wantAny: true},
		{name: "link is not followed", ctx: context.Background(), path: link, wantAny: true},
		{name: "cancelled context", ctx: cancelled, path: file, wantErr: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TakeSnapshot(tt.ctx, tt.path)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAny:
				if err == nil {
					t.Fatal("want an error")
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if got.Size != tt.want.Size || got.SHA256 != tt.want.SHA256 || !got.ModTime.Equal(tt.want.ModTime) {
					t.Errorf("got %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestTakeSnapshot_CancelledDuringRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "big.docx")
	write(t, file, string(make([]byte, 3*copyBufSize)), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel as soon as the first chunk has been read.
	if _, err := copyCtx(ctx, hookWriter{discard{}, func() error { cancel(); return nil }}, mustOpen(t, file), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func mustOpen(t *testing.T, p string) *os.File {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func TestSnapshot_JSON(t *testing.T) {
	in := Snapshot{Size: 42, ModTime: time.Date(2026, 11, 11, 10, 20, 30, 123456789, time.UTC), SHA256: "ab12"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	const wantJSON = `{"size":42,"modTime":"2026-11-11T10:20:30.123456789Z","sha256":"ab12"}`
	if string(b) != wantJSON {
		t.Errorf("json = %s, want %s", b, wantJSON)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Size != in.Size || out.SHA256 != in.SHA256 || !out.ModTime.Equal(in.ModTime) {
		t.Errorf("round trip: got %+v, want %+v", out, in)
	}
}
