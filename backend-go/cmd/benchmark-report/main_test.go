package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunPreservesSourceConfig(t *testing.T) {
	for _, test := range []struct {
		name, contents string
		mode           os.FileMode
	}{
		{"readonly-defaults", "{}\n", 0400},
		{"writable-legacy-channel", `{"autopilot":null,"upstream":[{"name":"legacy","baseUrl":"https://example.invalid","apiKeys":["test-only-key"],"serviceType":"claude"}]}`, 0644},
	} {
		t.Run(test.name, func(t *testing.T) {
			contents := test.contents
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(contents), test.mode); err != nil {
				t.Fatal(err)
			}
			oldTime := time.Unix(1700000000, 0)
			if err := os.Chtimes(path, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := run(path); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, []byte(contents)) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
				t.Fatal("report changed source config content, mtime or permissions")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("report created files beside original config: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestReportConfigUsesPrivateSnapshotAndCleansUp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := []byte(`{"autopilot":{"reasoningEffort":{"enabled":true,"expandVariants":true,"perTaskClass":{"worker":["high"]}}}}`)
	if err := os.WriteFile(path, data, 0400); err != nil {
		t.Fatal(err)
	}
	manager, cleanup, err := loadReportConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	routing := manager.GetAutopilotRouting()
	if !routing.ReasoningEffort.Enabled || !routing.ReasoningEffort.ExpandVariants || !reflect.DeepEqual(routing.ReasoningEffort.PerTaskClass["worker"], []string{"high"}) {
		t.Fatalf("snapshot lost explicit reasoning configuration: %+v", routing.ReasoningEffort)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot not created: entries=%v err=%v", entries, err)
	}
	snapshot := filepath.Join(tmp, entries[0].Name())
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{snapshot, 0700}, {filepath.Join(snapshot, "config.json"), 0600}} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("snapshot permissions are not private: %s: %v", item.path, err)
		}
	}
	// 修改源文件后，报告管理器仍保留启动时的配置快照。
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if !manager.GetAutopilotRouting().ReasoningEffort.ExpandVariants {
		t.Fatal("snapshot followed source config changes")
	}
	cleanup()
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("snapshot survived cleanup: %v", err)
	}
}

func TestReportConfigCleansUpAfterDecodeFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadReportConfig(path); err == nil {
		t.Fatal("expected invalid config error")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed decode leaked snapshot: entries=%v err=%v", entries, err)
	}
}

func TestCLIReportsOnStdoutAndRejectsMissingConfig(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "report", false: "missing"}[valid], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if valid {
				if err := os.WriteFile(path, []byte("{}"), 0400); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(os.Args[0], "-test.run=^TestReportCLIProcess$")
			command.Env = append(os.Environ(), "YUNQI_REPORT_TEST_PROCESS=1", "YUNQI_REPORT_TEST_CONFIG="+path)
			stdout, err := command.Output()
			if valid {
				if err != nil || !strings.Contains(string(stdout), "[BenchmarkSelectionReport]") {
					t.Fatalf("missing report on stdout: %v: %s", err, stdout)
				}
				contents, err := os.ReadFile(path)
				if err != nil || string(contents) != "{}" {
					t.Fatalf("CLI changed original config: %v", err)
				}
			} else {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
					t.Fatalf("expected CLI exit 1, got %v", err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("CLI created missing config: %v", err)
				}
			}
		})
	}
}

func TestReportCLIProcess(t *testing.T) {
	if os.Getenv("YUNQI_REPORT_TEST_PROCESS") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("benchmark-report", flag.ExitOnError)
	os.Args = []string{"benchmark-report", "-config", os.Getenv("YUNQI_REPORT_TEST_CONFIG")}
	main()
	os.Exit(0)
}

func TestRunRejectsMissingAndInvalidConfigWithoutCreatingFiles(t *testing.T) {
	for _, contents := range []string{"missing", "{invalid"} {
		t.Run(contents, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if contents != "missing" {
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := run(path); err == nil {
				t.Fatal("expected error for unusable config")
			}
			entries, err := os.ReadDir(dir)
			want := 1
			if contents == "missing" {
				want = 0
			}
			if err != nil || len(entries) != want {
				t.Fatalf("report modified config directory: entries=%v err=%v", entries, err)
			}
		})
	}
}
