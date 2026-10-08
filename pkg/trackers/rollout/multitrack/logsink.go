package multitrack

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// EventType 描述 multitracker 输出的事件类别。
// aiops 平台可据此决定事件在发布日志中的呈现方式
// （例如 ResourceStatus 逐条追加、ResourceLog 按 Pod/容器分组折叠）。
type EventType string

const (
	// EventResourceStatus：资源状态快照（周期性或状态变化时发出）。
	EventResourceStatus EventType = "resource_status"
	// EventResourceLog：资源容器日志块（Pod 日志流）。
	EventResourceLog EventType = "resource_log"
	// EventResourceServiceMessage：资源级服务消息（added/ready 等）。
	EventResourceServiceMessage EventType = "resource_service_message"
	// EventResourceEvent：K8s Event 转发（Warning/Normal 事件）。
	EventResourceEvent EventType = "resource_event"
	// EventResourceError：资源跟踪失败原因。
	EventResourceError EventType = "resource_error"
	// EventResourceWarning：资源跟踪警告（如 HopeUntilEndOfDeployProcess 过程中的错误计数）。
	EventResourceWarning EventType = "resource_warning"
	// EventTrackingSummary：整个 multitrack 会话的阶段性汇总（失败资源清单等）。
	EventTrackingSummary EventType = "tracking_summary"
)

// Level 事件日志级别，与常规日志级别对齐。
type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Event 是 multitracker 输出的结构化日志事件。
// 所有字段均为 JSON 友好类型，便于平台直接序列化入库/展示。
type Event struct {
	// Time 事件产生时间（RFC3339）。
	Time time.Time `json:"time"`
	// Type 事件类别。
	Type EventType `json:"type"`
	// Level 日志级别。
	Level Level `json:"level"`
	// Resource 资源标识（如 deploy/myapp、sts/db-0、po/mypod-abc container/app）。
	// 汇总类事件可为空。
	Resource string `json:"resource,omitempty"`
	// Namespace 资源所在命名空间（可为空）。
	Namespace string `json:"namespace,omitempty"`
	// Message 人类可读消息正文（单行）。
	Message string `json:"message"`
	// Data 附加结构化数据（各事件类别自定义载荷）。
	Data map[string]any `json:"data,omitempty"`
}

// LogSink 接收 multitracker 的结构化日志事件。
// 实现必须并发安全（multitracker 多 goroutine 并发输出）。
type LogSink interface {
	WriteEvent(e *Event)
}

// FuncLogSink 将函数适配为 LogSink。
type FuncLogSink func(e *Event)

func (f FuncLogSink) WriteEvent(e *Event) { f(e) }

// JSONLogSink 将事件序列化为 JSON Lines 写入 io.Writer（默认 stdout）。
// 每行一个 JSON 对象，无 ANSI 转义、无终端宽度依赖，适合管道消费：
//
//	kubedog | jq 'select(.type=="resource_status")'
type JSONLogSink struct {
	mu   sync.Mutex
	dest io.Writer
}

// NewJSONLogSink 创建写出到 w 的 JSON Lines 日志接收器。
// w 为 nil 时写 stdout。
func NewJSONLogSink(w io.Writer) *JSONLogSink {
	if w == nil {
		w = os.Stdout
	}
	return &JSONLogSink{dest: w}
}

func (s *JSONLogSink) WriteEvent(e *Event) {
	if e == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}

	data, err := json.Marshal(e)
	if err != nil {
		// 兜底：事件序列化失败时输出最小化可读行，绝不静默丢失
		fmt.Fprintf(os.Stderr, "kubedog: log event marshal failed: %v (type=%s resource=%s)\n", err, e.Type, e.Resource)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.dest.Write(append(data, '\n'))
}

// NewDefaultLogSink 返回默认日志接收器：JSON Lines 写 stdout。
func NewDefaultLogSink() LogSink {
	return NewJSONLogSink(os.Stdout)
}

var _ LogSink = (*JSONLogSink)(nil)
var _ LogSink = FuncLogSink(nil)
