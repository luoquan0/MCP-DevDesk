package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const (
	// MaxEntries is the maximum number of newline-delimited records retained
	// in any MCP DevDesk log file.
	MaxEntries = 100
	// MaxBytes protects against a single abnormally large log line.
	MaxBytes = int64(2 << 20)
)

type EnabledFunc func() bool

var logPathLocks sync.Map

// FileWriter appends to a log file while keeping only the newest bounded
// history. When enabled returns false, writes are accepted and discarded so
// child processes can continue running without blocking.
type FileWriter struct {
	mu       sync.Mutex
	path     string
	enabled  EnabledFunc
	loaded   bool
	data     []byte
	diskSize int64
}

func NewFileWriter(path string, enabled EnabledFunc) (*FileWriter, error) {
	w := &FileWriter{path: filepath.Clean(path), enabled: enabled}
	if !w.isEnabled() {
		return w, nil
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return nil, err
	}
	unlock := lockLogPath(w.path)
	defer unlock()
	if err := w.loadLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *FileWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if !w.isEnabled() {
		return len(data), nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	unlock := lockLogPath(w.path)
	defer unlock()
	if !w.isEnabled() {
		return len(data), nil
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return 0, err
	}
	if !w.loaded || fileSizeChanged(w.path, w.diskSize) {
		if err := w.loadLocked(); err != nil {
			return 0, err
		}
	}
	combined := make([]byte, 0, len(w.data)+len(data))
	combined = append(combined, w.data...)
	combined = append(combined, data...)
	retained := boundedLogData(combined)
	if len(retained) < len(combined) {
		if err := os.WriteFile(w.path, retained, 0o600); err != nil {
			return 0, err
		}
		w.diskSize = int64(len(retained))
	} else {
		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return 0, err
		}
		written, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return written, writeErr
		}
		if closeErr != nil {
			return written, closeErr
		}
		w.diskSize += int64(written)
	}
	w.data = append(w.data[:0], retained...)
	return len(data), nil
}

func (w *FileWriter) Close() error {
	return nil
}

func (w *FileWriter) isEnabled() bool {
	return w.enabled == nil || w.enabled()
}

func (w *FileWriter) loadLocked() error {
	if err := trimFileUnlocked(w.path); err != nil {
		return err
	}
	raw, err := os.ReadFile(w.path)
	if errors.Is(err, os.ErrNotExist) {
		w.data = nil
		w.diskSize = 0
		w.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	w.data = append(w.data[:0], raw...)
	w.diskSize = int64(len(raw))
	w.loaded = true
	return nil
}

func fileSizeChanged(path string, expected int64) bool {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return expected != 0
	}
	return err != nil || info.Size() != expected
}

func boundedLogData(data []byte) []byte {
	data = keepLastEntries(data, MaxEntries)
	if int64(len(data)) <= MaxBytes {
		return data
	}
	data = data[len(data)-int(MaxBytes):]
	if index := bytes.IndexByte(data, '\n'); index >= 0 {
		data = data[index+1:]
	}
	return data
}

// AppendLine appends a single newline-delimited record and trims the file.
func AppendLine(path string, line []byte) error {
	path = filepath.Clean(path)
	unlock := lockLogPath(path)
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if len(line) > 0 {
		if _, err := file.Write(line); err != nil {
			_ = file.Close()
			return err
		}
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		if _, err := file.Write([]byte{'\n'}); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	return trimFileUnlocked(path)
}

// TrimFile keeps only the newest MaxEntries and MaxBytes of an existing log.
func TrimFile(path string) error {
	path = filepath.Clean(path)
	unlock := lockLogPath(path)
	defer unlock()
	return trimFileUnlocked(path)
}

func trimFileUnlocked(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if info.Size() == 0 {
		_ = file.Close()
		return nil
	}

	readSize := info.Size()
	if readSize > MaxBytes {
		readSize = MaxBytes
	}
	buffer := make([]byte, int(readSize))
	start := info.Size() - readSize
	read, readErr := file.ReadAt(buffer, start)
	if readErr != nil && read != len(buffer) {
		_ = file.Close()
		return readErr
	}
	if err := file.Close(); err != nil {
		return err
	}
	buffer = buffer[:read]

	if start > 0 {
		if index := bytes.IndexByte(buffer, '\n'); index >= 0 {
			buffer = buffer[index+1:]
		}
	}
	buffer = keepLastEntries(buffer, MaxEntries)
	if int64(len(buffer)) > MaxBytes {
		buffer = buffer[len(buffer)-int(MaxBytes):]
		if index := bytes.IndexByte(buffer, '\n'); index >= 0 {
			buffer = buffer[index+1:]
		}
	}

	if info.Size() <= MaxBytes && countEntries(buffer) <= MaxEntries && int64(len(buffer)) == info.Size() {
		return nil
	}
	return os.WriteFile(path, buffer, 0o600)
}

func lockLogPath(path string) func() {
	cleaned := filepath.Clean(path)
	value, _ := logPathLocks.LoadOrStore(cleaned, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func keepLastEntries(data []byte, limit int) []byte {
	if limit <= 0 || len(data) == 0 || countEntries(data) <= limit {
		return data
	}
	index := len(data) - 1
	if data[index] == '\n' {
		index--
	}
	boundaries := 0
	for ; index >= 0; index-- {
		if data[index] != '\n' {
			continue
		}
		boundaries++
		if boundaries == limit {
			return data[index+1:]
		}
	}
	return data
}

func countEntries(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	entries := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		entries++
	}
	return entries
}
