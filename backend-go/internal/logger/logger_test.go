package logger

import (
	"github.com/soulteary/yunqi/internal/redaction"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetLoggersForTest() {
	log.SetOutput(io.Discard)
	if rawFileLog != nil {
		if closer, ok := rawFileLog.Writer().(io.Closer); ok {
			_ = closer.Close()
		}
	}
	rawFileLog = nil
	consoleLog = nil
}

func TestSetupLogDirNoneConsoleTrue(t *testing.T) {
	const secret = "opaque!console-credential=secret"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original; _ = reader.Close(); _ = writer.Close(); resetLoggersForTest() }()
	if err := Setup(&Config{LogDir: "none", Console: true}); err != nil {
		t.Fatal(err)
	}
	if rawFileLog == nil || rawFileLog.Writer() != io.Discard {
		t.Fatal("disabled file channel must discard")
	}
	ConsoleLog().Println(secret)
	log.Println(secret)
	_ = writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), secret) || strings.Count(string(output), redaction.Mask) != 2 {
		t.Fatalf("unsafe console output: %s", output)
	}
}

func TestSetupLogDirNoneConsoleFalse(t *testing.T) {
	cfg := &Config{
		LogDir:  "none",
		Console: false,
	}

	err := Setup(cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(resetLoggersForTest)

	// 不应创建目录
	if _, err := os.Stat("none"); !os.IsNotExist(err) {
		t.Fatal("Setup() created directory 'none', should not")
	}

	// rawFileLog 应写入 io.Discard
	if rawFileLog == nil {
		t.Fatal("rawFileLog is nil")
	}
	if rawFileLog.Writer() != io.Discard {
		t.Fatalf("rawFileLog writer = %v, want io.Discard", rawFileLog.Writer())
	}

	// consoleLog 应写入 io.Discard
	if consoleLog == nil {
		t.Fatal("consoleLog is nil")
	}
	if consoleLog.Writer() != io.Discard {
		t.Fatalf("consoleLog writer = %v, want io.Discard", consoleLog.Writer())
	}

	// 全局 log 应写入 io.Discard
	if log.Default().Writer() != io.Discard {
		t.Fatalf("global log writer = %v, want io.Discard", log.Default().Writer())
	}
}

func TestSetupLogDirNullConsoleTrue(t *testing.T) {
	cfg := &Config{
		LogDir:  "null",
		Console: true,
	}

	err := Setup(cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(resetLoggersForTest)

	// 不应创建目录
	if _, err := os.Stat("null"); !os.IsNotExist(err) {
		t.Fatal("Setup() created directory 'null', should not")
	}

	// rawFileLog 应写入 io.Discard
	if rawFileLog.Writer() != io.Discard {
		t.Fatalf("rawFileLog writer = %v, want io.Discard", rawFileLog.Writer())
	}
}

func TestSetupNormalLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		LogDir:     tmpDir,
		LogFile:    "test.log",
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
		Compress:   false,
		Console:    true,
	}

	err := Setup(cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	t.Cleanup(resetLoggersForTest)

	// 应创建日志文件
	logPath := filepath.Join(tmpDir, "test.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}

	// rawFileLog 不应是 io.Discard
	if rawFileLog.Writer() == io.Discard {
		t.Fatal("rawFileLog should not be io.Discard for normal log dir")
	}
}

func TestIsLogDisabled(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"none", true},
		{"null", true},
		{"NONE", true},
		{"NULL", true},
		{"None", true},
		{"Null", true},
		{" none ", true},
		{" null ", true},
		{"logs", false},
		{"/var/log/ccx", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsLogDisabled(tt.input)
			if got != tt.want {
				t.Fatalf("IsLogDisabled(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEveryFileLogChannelRedactsCredentials(t *testing.T) {
	const secret = "opaque!file-credential=secret"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	dir := t.TempDir()
	if err := Setup(&Config{LogDir: dir, LogFile: "redacted.log", Console: false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resetLoggersForTest)
	log.Println(secret)
	RawFileLog().Println(secret)
	ConsoleLog().Println(secret)
	data, err := os.ReadFile(filepath.Join(dir, "redacted.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Count(string(data), redaction.Mask) != 3 {
		t.Fatalf("unsafe log file: %s", data)
	}
}
