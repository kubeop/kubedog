package multitrack

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONLogSinkWriteEvent(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONLogSink(&buf)

	sink.WriteEvent(&Event{
		Type:     EventResourceStatus,
		Level:    LevelInfo,
		Resource: "deploy/myapp",
		Message:  "deploy/myapp (replicas=3/3, ready=3/3)",
		Data:     map[string]any{"replicas": "3/3", "isReady": true},
	})
	sink.WriteEvent(nil) // 应被忽略

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("event is not valid JSON: %v", err)
	}

	if e.Type != EventResourceStatus {
		t.Errorf("type = %q, want %q", e.Type, EventResourceStatus)
	}
	if e.Resource != "deploy/myapp" {
		t.Errorf("resource = %q, want deploy/myapp", e.Resource)
	}
	if e.Level != LevelInfo {
		t.Errorf("level = %q, want info", e.Level)
	}
	if e.Data["replicas"] != "3/3" {
		t.Errorf("data.replicas = %v, want 3/3", e.Data["replicas"])
	}
	if e.Time.IsZero() {
		t.Error("time should be auto-filled")
	}

	if strings.Contains(lines[0], "\x1b") {
		t.Error("output must not contain ANSI escape sequences")
	}
}

func TestFuncLogSink(t *testing.T) {
	var got *Event
	sink := FuncLogSink(func(e *Event) { got = e })

	sink.WriteEvent(&Event{Type: EventResourceLog, Resource: "po/app-1", Message: "line"})

	if got == nil || got.Type != EventResourceLog {
		t.Fatalf("FuncLogSink did not deliver event: %+v", got)
	}
}
