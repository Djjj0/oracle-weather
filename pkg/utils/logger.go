package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

var Logger *logrus.Logger

// SetupLogger initializes and configures the logger
func SetupLogger(logLevel string) *logrus.Logger {
	Logger = logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	Logger.SetLevel(level)

	// Set formatter for console output
	Logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		ForceColors:     true,
	})

	// Create logs directory if it doesn't exist
	logsDir := "data/logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		Logger.Warnf("Failed to create logs directory: %v", err)
		return Logger
	}

	// Create log file with daily rotation
	logFileName := fmt.Sprintf("bot_%s.log", time.Now().Format("2006-01-02"))
	logFilePath := filepath.Join(logsDir, logFileName)

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		Logger.Warnf("Failed to open log file: %v", err)
		return Logger
	}

	// Write to both console and file
	Logger.SetOutput(logFile)

	// Also log to console
	Logger.AddHook(&ConsoleHook{})

	Logger.Infof("Logger initialized - Level: %s, File: %s", logLevel, logFilePath)

	return Logger
}

// ConsoleHook is a custom hook to write logs to console as well
type ConsoleHook struct{}

func (hook *ConsoleHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (hook *ConsoleHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}
	fmt.Print(line)
	return nil
}
