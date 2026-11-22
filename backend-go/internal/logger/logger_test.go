package logger

import (
	"bytes"
	"encoding/json"
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

func TestApplicationWriterProducesOneStructuredRecord(t *testing.T) {
	var output bytes.Buffer
	writer := newApplicationWriter(&output)
	message := "warning: first line\nsecond line\n"
	if n, err := writer.Write([]byte(message)); err != nil || n != len(message) {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("expected one JSON record: %s", output.Bytes())
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("invalid structured record: %v", err)
	}
	if record["message"] != strings.TrimSuffix(message, "\n") || record["time"] == nil {
		t.Fatalf("unexpected record: %#v", record)
	}
	if _, ok := record["level"]; ok {
		t.Fatalf("legacy messages must not infer severity: %#v", record)
	}
}

func TestApplicationWriterUsesCurrentRedactionRules(t *testing.T) {
	var output bytes.Buffer
	writer := newApplicationWriter(&output)
	const secret = "opaque\"credential\\value"
	redaction.Configure([]string{secret})
	t.Cleanup(func() { redaction.Configure(nil) })
	_, _ = writer.Write([]byte(secret + "\n"))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["message"] != redaction.Mask {
		t.Fatalf("secret was not redacted before JSON encoding: %#v", record)
	}
}

func TestSetupPreservesRawFileFormatAndStandardLogCapture(t *testing.T) {
	dir := t.TempDir()
	if err := Setup(&Config{LogDir: dir, LogFile: "channels.log", Console: false}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resetLoggersForTest)
	log.Print("application-record")
	RawFileLog().Print(`{"raw-marker":true}`)
	data, err := os.ReadFile(filepath.Join(dir, "channels.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "application-record") != 1 || strings.Count(string(data), "raw-marker") != 1 {
		t.Fatalf("duplicate or missing records: %s", data)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, "raw-marker") && !strings.HasSuffix(line, `{"raw-marker":true}`) {
			t.Fatalf("raw record was JSON encoded again: %s", line)
		}
	}
	var capture bytes.Buffer
	log.SetOutput(&capture)
	log.Print("captured-record")
	if capture.String() != "captured-record\n" {
		t.Fatalf("standard log capture was bypassed: %s", capture.Bytes())
	}
}

func TestSetupKeepsFileAndConsoleChannelsSeparate(t *testing.T) {
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
		_ = reader.Close()
		_ = writer.Close()
		resetLoggersForTest()
	})
	dir := t.TempDir()
	if err := Setup(&Config{LogDir: dir, LogFile: "channels.log", Console: true}); err != nil {
		t.Fatal(err)
	}
	log.Print("shared-application-marker")
	RawFileLog().Print(`{"file-only-marker":true}`)
	ConsoleLog().Print("console-only-marker")
	_ = writer.Close()
	console, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(dir, "channels.log"))
	if err != nil {
		t.Fatal(err)
	}
	for destination, data := range map[string][]byte{"file": file, "console": console} {
		if strings.Count(string(data), "shared-application-marker") != 1 {
			t.Fatalf("application log duplicated or absent in %s: %s", destination, data)
		}
	}
	if strings.Contains(string(file), "console-only-marker") || strings.Contains(string(console), "file-only-marker") {
		t.Fatalf("log channels crossed: file=%s console=%s", file, console)
	}
}
