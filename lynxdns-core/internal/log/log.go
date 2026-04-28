package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn":
		return WARN
	case "error":
		return ERROR
	default:
		return INFO
	}
}

type Entry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     Level     `json:"level"`
	Message   string    `json:"message"`
}

type Logger struct {
	mu      sync.Mutex
	level   Level
	outputs []io.Writer
	subscribers []chan Entry
}

var defaultLogger = New(INFO)

func New(level Level) *Logger {
	return &Logger{
		level:       level,
		outputs:     []io.Writer{os.Stdout},
		subscribers: make([]chan Entry, 0),
	}
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

func (l *Logger) AddOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outputs = append(l.outputs, w)
}

func (l *Logger) Subscribe() chan Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan Entry, 256)
	l.subscribers = append(l.subscribers, ch)
	return ch
}

func (l *Logger) Unsubscribe(ch chan Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, sub := range l.subscribers {
		if sub == ch {
			l.subscribers = append(l.subscribers[:i], l.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	entry := Entry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}

	line := fmt.Sprintf("[%s] %s %s\n",
		entry.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		entry.Level.String(),
		entry.Message,
	)

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, w := range l.outputs {
		w.Write([]byte(line))
	}

	for _, sub := range l.subscribers {
		select {
		case sub <- entry:
		default:
		}
	}
}

func (l *Logger) Debug(format string, args ...interface{}) { l.log(DEBUG, format, args...) }
func (l *Logger) Info(format string, args ...interface{})  { l.log(INFO, format, args...) }
func (l *Logger) Warn(format string, args ...interface{})  { l.log(WARN, format, args...) }
func (l *Logger) Error(format string, args ...interface{}) { l.log(ERROR, format, args...) }

func SetLevel(level Level)            { defaultLogger.SetLevel(level) }
func AddOutput(w io.Writer)           { defaultLogger.AddOutput(w) }
func Subscribe() chan Entry           { return defaultLogger.Subscribe() }
func Unsubscribe(ch chan Entry)       { defaultLogger.Unsubscribe(ch) }
func Debug(format string, args ...interface{}) { defaultLogger.Debug(format, args...) }
func Info(format string, args ...interface{})  { defaultLogger.Info(format, args...) }
func Warn(format string, args ...interface{})  { defaultLogger.Warn(format, args...) }
func Error(format string, args ...interface{}) { defaultLogger.Error(format, args...) }
