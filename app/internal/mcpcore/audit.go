package mcpcore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type auditLogger struct {
	mu   sync.Mutex
	path string
}

type auditRecord struct {
	Timestamp  string         `json:"timestamp"`
	Tool       string         `json:"tool"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"durationMs"`
}

func newAuditLogger(path string) *auditLogger {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &auditLogger{path: filepath.Clean(path)}
}

func (l *auditLogger) log(tool string, arguments map[string]any, started time.Time, err error) {
	if l == nil {
		return
	}
	record := auditRecord{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Tool:       tool,
		Arguments:  redactAuditArguments(arguments),
		Success:    err == nil,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if err != nil {
		record.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if os.MkdirAll(filepath.Dir(l.path), 0o700) != nil {
		return
	}
	file, openErr := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}

func redactAuditArguments(arguments map[string]any) map[string]any {
	if len(arguments) == 0 {
		return nil
	}
	result := make(map[string]any, len(arguments))
	for key, value := range arguments {
		lower := strings.ToLower(key)
		switch {
		case strings.Contains(lower, "password"), strings.Contains(lower, "secret"), strings.Contains(lower, "token"):
			result[key] = "***"
		case lower == "content", lower == "text", lower == "newtext", lower == "oldtext", lower == "stdin", lower == "chars", lower == "patch", lower == "data", lower == "dataurl", lower == "download_url", lower == "cmd":
			text, _ := value.(string)
			digest := sha256.Sum256([]byte(text))
			result[key] = map[string]any{
				"redacted": true,
				"bytes":    len(text),
				"sha256":   hex.EncodeToString(digest[:]),
			}
		case lower == "env", lower == "args":
			result[key] = "***"
		default:
			result[key] = redactNested(value)
		}
	}
	return result
}

func redactNested(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactAuditArguments(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactNested(item)
		}
		return result
	default:
		return value
	}
}
