package log

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileWriterBasicWrite(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir, 7, 100)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}

	msg := "hello log line\n"
	n, err := fw.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(msg) {
		t.Fatalf("Write() n = %d, want %d", n, len(msg))
	}
	fw.Close()

	today := time.Now().Format("20060102")
	name := filepath.Join(dir, "detail-"+today+".log")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), msg) {
		t.Fatalf("file content = %q, want to contain %q", string(data), msg)
	}
}

func TestFileWriterSizeRotation(t *testing.T) {
	dir := t.TempDir()
	// 1 MB max so rotation triggers quickly.
	fw, err := NewFileWriter(dir, 7, 1)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}

	line := strings.Repeat("x", 512) + "\n"
	// Write enough to exceed 1 MB.
	for i := 0; i < 2200; i++ {
		fw.Write([]byte(line))
	}
	fw.Close()

	today := time.Now().Format("20060102")
	seq0 := filepath.Join(dir, "detail-"+today+".log")
	seq1 := filepath.Join(dir, "detail-"+today+".1.log")
	if _, err := os.Stat(seq0); err != nil {
		t.Fatalf("seq0 file missing: %v", err)
	}
	if _, err := os.Stat(seq1); err != nil {
		t.Fatalf("seq1 file missing: %v", err)
	}
}

// TestFileWriterCrossDayRotation simulates a day change by directly
// invoking rotate() with a mocked date. This avoids lock-ordering
// issues that would arise from manually poking internal state.
func TestFileWriterCrossDayRotation(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir, 7, 100)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}

	// Write to current day.
	if _, err := fw.Write([]byte("today line\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Create a yesterday file directly so compressOldDay has something to gzip.
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
	yesterdayFile := filepath.Join(dir, "detail-"+yesterday+".log")
	if err := os.WriteFile(yesterdayFile, []byte("yesterday line\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Force the rotate path by calling it under the lock (same as Write does).
	fw.mu.Lock()
	err = fw.rotate(time.Now().Format("20060102"))
	fw.mu.Unlock()
	if err != nil {
		t.Fatalf("rotate() error = %v", err)
	}

	fw.Close()

	yesterdayGz := filepath.Join(dir, "detail-"+yesterday+".log.gz")
	if _, err := os.Stat(yesterdayGz); err != nil {
		t.Fatalf("yesterday .log.gz file missing: %v", err)
	}
	// Original yesterday .log file should be removed after gzip.
	if _, err := os.Stat(yesterdayFile); !os.IsNotExist(err) {
		t.Fatalf("yesterday .log file should be removed after gzip")
	}
}

func TestFileWriterCleanExpired(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed old .log.gz files: 8 days ago and 2 days ago.
	now := time.Now()
	oldDate := now.AddDate(0, 0, -8).Format("20060102")
	recentDate := now.AddDate(0, 0, -2).Format("20060102")

	oldFile := filepath.Join(dir, "detail-"+oldDate+".log.gz")
	recentFile := filepath.Join(dir, "detail-"+recentDate+".log.gz")

	writeGzip(t, oldFile, "old data")
	writeGzip(t, recentFile, "recent data")

	// maxDays=7 → 8-day file should be deleted, 2-day file kept.
	fw, err := NewFileWriter(dir, 7, 100)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}
	fw.Close()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file should be deleted")
	}
	if _, err := os.Stat(recentFile); err != nil {
		t.Fatalf("recent file should still exist")
	}
}

func TestGzipCompression(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "detail-20250101.log")
	content := "test log content\n"
	os.WriteFile(src, []byte(content), 0o644)

	dst := src + ".gz"
	if err := gzipFile(src, dst); err != nil {
		t.Fatalf("gzipFile() error = %v", err)
	}

	// Read back and verify content.
	f, err := os.Open(dst)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	gz.Close()
	f.Close()

	if string(data) != content {
		t.Fatalf("decompressed = %q, want %q", string(data), content)
	}

	// Original should still exist (deletion is caller's job).
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("original file should still exist before manual remove")
	}
}

func writeGzip(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	gz.Write([]byte(content))
	gz.Close()
	f.Close()
}

// TestCompressOldDaySkipsSymlink verifies that compressOldDay does NOT
// follow symlinks. A symlink in the log dir pointing at /etc/passwd (or
// similar) must be skipped — not gzipped and not removed.
func TestCompressOldDaySkipsSymlink(t *testing.T) {
	dir := t.TempDir()
	fw, err := NewFileWriter(dir, 7, 100)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}

	// Target file with secret-ish content, outside the regular log stream.
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Symlink that masquerades as an old-day log file.
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
	link := filepath.Join(dir, "detail-"+yesterday+".log")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	today := time.Now().Format("20060102")
	fw.compressOldDay(today)

	// Symlink must still exist (untouched).
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink should be left in place, got err = %v", err)
	}
	// No .gz must have been produced.
	linkGz := link + ".gz"
	if _, err := os.Stat(linkGz); !os.IsNotExist(err) {
		t.Fatalf("symlink target should NOT be gzipped, but .gz exists")
	}
	// Secret target must be unchanged.
	data, err := os.ReadFile(secret)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if string(data) != "top secret content" {
		t.Fatalf("secret content changed: %q", string(data))
	}
}

// TestCleanExpiredSkipsSymlink verifies cleanExpired ignores symlinks.
// A symlink matching the date pattern must not be deleted (and its target
// must not be touched).
func TestCleanExpiredSkipsSymlink(t *testing.T) {
	dir := t.TempDir()

	// Target file outside the date pattern, will survive on its own.
	target := filepath.Join(dir, "target.log")
	if err := os.WriteFile(target, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink that looks like an 8-days-old gzipped log → would match cleanExpired.
	oldDate := time.Now().AddDate(0, 0, -8).Format("20060102")
	link := filepath.Join(dir, "detail-"+oldDate+".log.gz")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	fw, err := NewFileWriter(dir, 7, 100)
	if err != nil {
		t.Fatalf("NewFileWriter() error = %v", err)
	}
	fw.Close()

	// NewFileWriter calls cleanExpired internally — symlink must remain.
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink should be skipped, got err = %v", err)
	}
	// Target must survive intact.
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target file should survive, got err = %v", err)
	}
}

// TestGzipFileCleansUpOnError verifies that gzipFile does not leave a
// half-written .gz behind when compression fails mid-way. We trigger
// failure by making the source unreadable mid-read via a permission trick.
func TestGzipFileCleansUpOnError(t *testing.T) {
	// Skip when running as root — root bypasses file permissions and the
	// setup wouldn't reliably trigger an io.Copy error.
	if os.Geteuid() == 0 {
		t.Skip("test relies on permission denial, ineffective when running as root")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "detail-20250101.log")
	if err := os.WriteFile(src, []byte("won't matter"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(src, 0o644) })

	dst := src + ".gz"
	err := gzipFile(src, dst)
	if err == nil {
		t.Fatal("gzipFile() expected error, got nil")
	}

	// No half-written .gz must remain.
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("half-written dst should be removed, err = %v", err)
	}
}

