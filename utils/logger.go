package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 日志级别常量
const (
	LogLevelDebug   = "debug"
	LogLevelInfo    = "info"
	LogLevelWarn    = "warn"
	LogLevelError   = "error"
	LogLevelStartup = "startup" // 启动级别，必然输出
)

// levelOrder 级别排序，用于 shouldLog 比较
var levelOrder = map[string]int{
	LogLevelDebug: 0,
	LogLevelInfo:  1,
	LogLevelWarn:  2,
	LogLevelError: 3,
}

// Logger 日志实例
// HTTP 处理器可能并发写日志，所有写操作必须持有 mu，
// 避免与日志轮转时的文件指针替换产生竞争
type Logger struct {
	mu            sync.Mutex
	file          *os.File
	logPath       string
	maxSize       int64  // 单文件最大字节数，超过后轮转为 .old
	logLevel      string // 日志级别
	consoleOutput bool   // 是否同时输出到控制台
}

// NewLogger 创建日志实例
func NewLogger(logPath string, maxSize int64, logLevel string, consoleOutput bool) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %v", err)
	}

	if _, ok := levelOrder[logLevel]; !ok {
		logLevel = LogLevelWarn
	}

	return &Logger{
		file:          file,
		logPath:       logPath,
		maxSize:       maxSize,
		logLevel:      logLevel,
		consoleOutput: consoleOutput,
	}, nil
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// shouldLog 判断该级别是否应当输出；startup 级别不受级别限制
func (l *Logger) shouldLog(level string) bool {
	if level == LogLevelStartup {
		return true
	}
	cur, ok1 := levelOrder[l.logLevel]
	msg, ok2 := levelOrder[level]
	if !ok1 || !ok2 {
		return true
	}
	return msg >= cur
}

// log 统一写入入口：写文件 + 按需写控制台
func (l *Logger) log(prefix, format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	if prefix != "" {
		msg = prefix + msg
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotateLocked(); err != nil {
		fmt.Printf("日志轮转失败: %v\n", err)
	}

	entry := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), msg)
	if _, err := l.file.WriteString(entry); err != nil {
		fmt.Printf("写日志失败: %v\n", err)
		return
	}
	_ = l.file.Sync()

	if l.consoleOutput {
		fmt.Print(entry)
	}
}

// rotateLocked 超过大小上限时轮转，调用方必须持有 l.mu
func (l *Logger) rotateLocked() error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= l.maxSize {
		return nil
	}

	if err := l.file.Close(); err != nil {
		return err
	}

	backup := l.logPath + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(l.logPath, backup); err != nil {
		return err
	}

	file, err := os.Create(l.logPath)
	if err != nil {
		return err
	}
	l.file = file
	return nil
}

// SetConsoleOutput 控制是否同时输出到控制台
func (l *Logger) SetConsoleOutput(enable bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enable
}

// Debug 调试级别日志
func (l *Logger) Debug(format string, v ...any) {
	if l.shouldLog(LogLevelDebug) {
		l.log("[DEBUG] ", format, v...)
	}
}

// Info 信息级别日志
func (l *Logger) Info(format string, v ...any) {
	if l.shouldLog(LogLevelInfo) {
		l.log("[INFO] ", format, v...)
	}
}

// Warn 警告级别日志
func (l *Logger) Warn(format string, v ...any) {
	if l.shouldLog(LogLevelWarn) {
		l.log("[WARN] ", format, v...)
	}
}

// Error 错误级别日志
func (l *Logger) Error(format string, v ...any) {
	if l.shouldLog(LogLevelError) {
		l.log("[ERROR] ", format, v...)
	}
}

// Startup 启动信息，必然输出
func (l *Logger) Startup(format string, v ...any) {
	l.log("[STARTUP] ", format, v...)
}

// 全局日志实例
var DefaultLogger *Logger

// InitLoggerWithLevel 按指定级别初始化全局日志实例
func InitLoggerWithLevel(logLevel string) error {
	logger, err := NewLogger("./data/fanxiaopai.log", 10*1024*1024, logLevel, true)
	if err != nil {
		return err
	}
	DefaultLogger = logger
	logger.Startup("日志系统初始化成功，日志级别: %s\n", logger.logLevel)
	return nil
}

// GetLogger 获取全局日志实例，未初始化时兜底创建一个
func GetLogger() *Logger {
	if DefaultLogger == nil {
		logger, err := NewLogger("./data/fanxiaopai.log", 10*1024*1024, LogLevelWarn, true)
		if err != nil {
			panic(fmt.Sprintf("初始化日志失败: %v", err))
		}
		DefaultLogger = logger
	}
	return DefaultLogger
}
