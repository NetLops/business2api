package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Level 日志级别
type Level int

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

var levelNames = map[Level]string{
	LevelError: "ERROR",
	LevelWarn:  "WARN",
	LevelInfo:  "INFO",
	LevelDebug: "DEBUG",
}

// Logger 日志记录器
type Logger struct {
	level  Level
	prefix string
	mu     sync.Mutex
}

var (
	defaultLogger = &Logger{level: LevelInfo}
	debugMode     = false
)

// SetDebugMode 设置调试模式
func SetDebugMode(debug bool) {
	debugMode = debug
	if debug {
		defaultLogger.level = LevelDebug
	} else {
		defaultLogger.level = LevelInfo
	}
}

// IsDebug 是否为调试模式
func IsDebug() bool {
	return debugMode
}

// SetLevel 设置日志级别
func SetLevel(level Level) {
	defaultLogger.level = level
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level > l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	levelStr := levelNames[level]
	msg := fmt.Sprintf(format, args...)

	if l.prefix != "" {
		log.Printf("[%s] [%s] [%s] %s", timestamp, levelStr, l.prefix, msg)
	} else {
		log.Printf("[%s] [%s] %s", timestamp, levelStr, msg)
	}
}

// Error 错误日志（始终输出）
func Error(format string, args ...interface{}) {
	defaultLogger.log(LevelError, format, args...)
}

// Warn 警告日志（始终输出）
func Warn(format string, args ...interface{}) {
	defaultLogger.log(LevelWarn, format, args...)
}

// Info 信息日志（正常模式输出）
func Info(format string, args ...interface{}) {
	defaultLogger.log(LevelInfo, format, args...)
}

// Debug 调试日志（仅debug模式输出）
func Debug(format string, args ...interface{}) {
	defaultLogger.log(LevelDebug, format, args...)
}

// WithPrefix 创建带前缀的子日志器
func WithPrefix(prefix string) *Logger {
	return &Logger{
		level:  defaultLogger.level,
		prefix: prefix,
	}
}

func (l *Logger) Error(format string, args ...interface{}) { l.log(LevelError, format, args...) }
func (l *Logger) Warn(format string, args ...interface{})  { l.log(LevelWarn, format, args...) }
func (l *Logger) Info(format string, args ...interface{})  { l.log(LevelInfo, format, args...) }
func (l *Logger) Debug(format string, args ...interface{}) { l.log(LevelDebug, format, args...) }

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
}
