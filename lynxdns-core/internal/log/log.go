package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

const (
	colorReset  = "\x1b[0m"
	colorRed    = "\x1b[31m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorBlue   = "\x1b[34m"
	colorPurple = "\x1b[35m"
	colorCyan   = "\x1b[36m"
	colorWhite  = "\x1b[37m"
)

const (
	ColorDomestic  = colorGreen
	ColorRemote    = colorCyan
	ColorCacheHit  = colorYellow
	ColorBlocked   = colorRed
	ColorAdBlock   = colorRed
	ColorLeakBlock = colorPurple
	ColorRuleBlock = colorRed
	ColorFailed    = colorRed
	ColorRedirect  = colorPurple
	ColorDefault   = colorWhite
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

type logItem struct {
	coloredLine string
	plainLine   string
	entry       Entry
}

type RotatingFileWriter struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	maxSize  int64
	currSize int64
}

func NewRotatingFileWriter(filePath string, maxSizeMB int) (*RotatingFileWriter, error) {
	rfw := &RotatingFileWriter{
		filePath: filePath,
		maxSize:  int64(maxSizeMB) * 1024 * 1024,
	}
	if err := rfw.openFile(); err != nil {
		return nil, err
	}
	return rfw, nil
}

func (r *RotatingFileWriter) openFile() error {
	f, err := os.OpenFile(r.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.file = f
	r.currSize = info.Size()
	return nil
}

func (r *RotatingFileWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.maxSize > 0 && r.currSize+int64(len(p)) > r.maxSize {
		r.rotate()
	}

	n, err := r.file.Write(p)
	if err == nil {
		r.currSize += int64(n)
	}
	return n, err
}

func (r *RotatingFileWriter) rotate() {
	r.file.Close()

	backupPath := r.filePath + ".1"
	os.Rename(r.filePath, backupPath)

	r.openFile()
}

func (r *RotatingFileWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

type Logger struct {
	mu          sync.Mutex
	level       atomic.Int32
	outputs     []io.Writer
	subscribers []chan Entry
	ch          chan logItem
	done        chan struct{}
	closed      atomic.Bool
	dropped     atomic.Int64
}

var defaultLogger = New(INFO)

func New(level Level) *Logger {
	l := &Logger{
		outputs:     []io.Writer{os.Stdout},
		subscribers: make([]chan Entry, 0),
		ch:          make(chan logItem, 4096),
		done:        make(chan struct{}),
	}
	l.level.Store(int32(level))
	go l.flushLoop()
	return l
}

func (l *Logger) flushLoop() {
	for item := range l.ch {
		l.mu.Lock()
		outputs := l.outputs
		subs := l.subscribers
		l.mu.Unlock()

		for _, w := range outputs {
			if w == os.Stdout || w == os.Stderr {
				w.Write([]byte(item.coloredLine))
			} else {
				w.Write([]byte(item.plainLine))
			}
		}

		for _, sub := range subs {
			select {
			case sub <- item.entry:
			default:
			}
		}
	}
	close(l.done)
}

func (l *Logger) SetLevel(level Level) {
	l.level.Store(int32(level))
}

func (l *Logger) GetLevel() Level {
	return Level(l.level.Load())
}

func (l *Logger) AddOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outputs = append(l.outputs, w)
}

func (l *Logger) SetOutputs(outputs []io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outputs = make([]io.Writer, len(outputs))
	copy(l.outputs, outputs)
}

func (l *Logger) Close() {
	if l.closed.Swap(true) {
		return
	}
	close(l.ch)
	<-l.done

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, w := range l.outputs {
		if w != os.Stdout && w != os.Stderr {
			if c, ok := w.(io.Closer); ok {
				c.Close()
			}
		}
	}
	l.outputs = []io.Writer{os.Stdout}
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
	if level < Level(l.level.Load()) {
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

	select {
	case l.ch <- logItem{coloredLine: line, plainLine: line, entry: entry}:
	default:
		l.dropped.Add(1)
	}
}

func (l *Logger) Debug(format string, args ...interface{}) { l.log(DEBUG, format, args...) }
func (l *Logger) Info(format string, args ...interface{})  { l.log(INFO, format, args...) }
func (l *Logger) Warn(format string, args ...interface{})  { l.log(WARN, format, args...) }
func (l *Logger) Error(format string, args ...interface{}) { l.log(ERROR, format, args...) }

func (l *Logger) ColorInfo(color string, format string, args ...interface{}) {
	l.logWithColor(INFO, color, format, args...)
}

func (l *Logger) ColorInfoMulti(labelColor string, format string, args ...interface{}) {
	l.logWithColorMulti(INFO, labelColor, format, args...)
}

func (l *Logger) logWithColor(level Level, color string, format string, args ...interface{}) {
	if level < Level(l.level.Load()) {
		return
	}

	entry := Entry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}

	coloredLine := fmt.Sprintf("[%s] %s%s%s %s%s%s\n",
		entry.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		colorReset,
		entry.Level.String(),
		colorReset,
		color,
		entry.Message,
		colorReset,
	)

	plainLine := fmt.Sprintf("[%s] %s %s\n",
		entry.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		entry.Level.String(),
		entry.Message,
	)

	select {
	case l.ch <- logItem{coloredLine: coloredLine, plainLine: plainLine, entry: entry}:
	default:
		l.dropped.Add(1)
	}
}

func (l *Logger) logWithColorMulti(level Level, labelColor string, format string, args ...interface{}) {
	if level < Level(l.level.Load()) {
		return
	}

	msg := fmt.Sprintf(format, args...)

	entry := Entry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   msg,
	}

	coloredLine := fmt.Sprintf("[%s] %s%s%s %s\n",
		entry.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		colorReset,
		entry.Level.String(),
		colorReset,
		msg,
	)

	plainLine := fmt.Sprintf("[%s] %s %s\n",
		entry.Timestamp.Format("2006-01-02T15:04:05.000Z"),
		entry.Level.String(),
		stripANSI(msg),
	)

	select {
	case l.ch <- logItem{coloredLine: coloredLine, plainLine: plainLine, entry: entry}:
	default:
		l.dropped.Add(1)
	}
}

func stripANSI(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					if (s[i] >= '0' && s[i] <= '9') || s[i] == ';' {
						i++
					} else {
						i++
						break
					}
				}
			}
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

func (l *Logger) DroppedCount() int64 {
	return l.dropped.Load()
}

func SetLevel(level Level)            { defaultLogger.SetLevel(level) }
func GetLevel() Level                 { return defaultLogger.GetLevel() }
func AddOutput(w io.Writer)           { defaultLogger.AddOutput(w) }
func SetOutputs(outputs []io.Writer)  { defaultLogger.SetOutputs(outputs) }
func Close()                          { defaultLogger.Close() }
func Subscribe() chan Entry           { return defaultLogger.Subscribe() }
func Unsubscribe(ch chan Entry)       { defaultLogger.Unsubscribe(ch) }
func Debug(format string, args ...interface{}) { defaultLogger.Debug(format, args...) }
func Info(format string, args ...interface{})  { defaultLogger.Info(format, args...) }
func Warn(format string, args ...interface{})  { defaultLogger.Warn(format, args...) }
func Error(format string, args ...interface{}) { defaultLogger.Error(format, args...) }
func ColorInfo(color string, format string, args ...interface{}) {
	defaultLogger.ColorInfo(color, format, args...)
}
func ColorInfoMulti(labelColor string, format string, args ...interface{}) {
	defaultLogger.ColorInfoMulti(labelColor, format, args...)
}
func DroppedCount() int64 { return defaultLogger.DroppedCount() }

func NewRotatingWriter(filePath string, maxSizeMB int) (*RotatingFileWriter, error) {
	dir := filepath.Dir(filePath)
	os.MkdirAll(dir, 0755)
	return NewRotatingFileWriter(filePath, maxSizeMB)
}
