package log

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileWriter writes log output to date-named files with size-based
// sequencing, gzip compression of past days, and retention-based cleanup.
type FileWriter struct {
	dir         string
	maxDays     int
	maxSize     int64 // bytes
	mu          sync.Mutex
	current     *os.File
	currentSize int64
	date        string
	seq         int
}

// dateRegexp matches "detail-YYYYMMDD" optionally followed by ".N" seq suffix,
// then ".log" or ".log.gz".
var dateRegexp = regexp.MustCompile(`^detail-(\d{4}\d{2}\d{2})(?:\.\d+)?\.log(?:\.gz)?$`)

// NewFileWriter creates a FileWriter that writes log files under dir.
// Files are named detail-YYYYMMDD.log (and detail-YYYYMMDD.N.log when
// the daily size limit is exceeded). maxSize is in MB.
func NewFileWriter(dir string, maxDays, maxSizeMB int) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	now := time.Now()
	d := now.Format("20060102")
	fw := &FileWriter{
		dir:     dir,
		maxDays: maxDays,
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		date:    d,
		seq:     0,
	}
	if err := fw.openCurrent(d, 0); err != nil {
		return nil, err
	}
	fw.compressOldDay(d)
	fw.cleanExpired()
	return fw, nil
}

func (fw *FileWriter) fileName(date string, seq int) string {
	if seq == 0 {
		return filepath.Join(fw.dir, fmt.Sprintf("detail-%s.log", date))
	}
	return filepath.Join(fw.dir, fmt.Sprintf("detail-%s.%d.log", date, seq))
}

func (fw *FileWriter) openCurrent(date string, seq int) error {
	name := fw.fileName(date, seq)
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", name, err)
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return fmt.Errorf("seek log file: %w", err)
	}
	fw.current = f
	fw.currentSize = size
	fw.date = date
	fw.seq = seq
	return nil
}

// Write implements io.Writer. Each write checks for day change and size limit.
func (fw *FileWriter) Write(p []byte) (int, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	today := time.Now().Format("20060102")
	if today != fw.date {
		if err := fw.rotate(today); err != nil {
			return 0, err
		}
	}

	if fw.currentSize+int64(len(p)) > fw.maxSize && fw.currentSize > 0 {
		// Size exceeded: close current and open next seq for same day.
		fw.current.Close()
		nextSeq := fw.seq + 1
		if err := fw.openCurrent(fw.date, nextSeq); err != nil {
			return 0, err
		}
	}

	n, err := fw.current.Write(p)
	fw.currentSize += int64(n)
	return n, err
}

func (fw *FileWriter) rotate(today string) error {
	if fw.current != nil {
		fw.current.Close()
	}
	// Compress files from previous days before opening new day's file.
	// Use today as the cutoff — anything not today gets gzipped.
	fw.compressOldDay(today)
	fw.cleanExpired()
	return fw.openCurrent(today, 0)
}

// compressOldDay gzips all .log files whose date is not today, removing
// the originals after successful compression.
func (fw *FileWriter) compressOldDay(today string) {
	entries, err := os.ReadDir(fw.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".gz") {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 || !e.Type().IsRegular() {
			continue
		}
		m := dateRegexp.FindStringSubmatch(name)
		if m == nil || m[1] == today {
			continue
		}
		src := filepath.Join(fw.dir, name)
		dst := src + ".gz"
		if err := gzipFile(src, dst); err != nil {
			continue
		}
		os.Remove(src)
	}
}

// cleanExpired deletes log files (both .log and .log.gz) older than maxDays.
func (fw *FileWriter) cleanExpired() {
	cutoff := time.Now().AddDate(0, 0, -fw.maxDays)
	cutoffStr := cutoff.Format("20060102")
	entries, err := os.ReadDir(fw.dir)
	if err != nil {
		return
	}
	var toDelete []string
	for _, e := range entries {
		name := e.Name()
		if e.Type()&os.ModeSymlink != 0 || !e.Type().IsRegular() {
			continue
		}
		m := dateRegexp.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if m[1] < cutoffStr {
			toDelete = append(toDelete, filepath.Join(fw.dir, name))
		}
	}
	for _, p := range toDelete {
		os.Remove(p)
	}
}

// Close closes the current log file.
func (fw *FileWriter) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.current != nil {
		return fw.current.Close()
	}
	return nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	gz.Name = filepath.Base(src)
	if _, err := io.Copy(gz, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// listLogDates returns sorted unique date strings from log file names in dir.
func listLogDates(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	dates := make(map[string]bool)
	for _, e := range entries {
		m := dateRegexp.FindStringSubmatch(e.Name())
		if m != nil {
			dates[m[1]] = true
		}
	}
	result := make([]string, 0, len(dates))
	for d := range dates {
		result = append(result, d)
	}
	sort.Strings(result)
	return result
}
