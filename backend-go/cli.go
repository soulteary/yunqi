package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/logger"
)

type cliAction int

const (
	cliActionRun cliAction = iota
	cliActionHelp
	cliActionVersion
)

const (
	defaultConfigPath              = ".config/config.json"
	defaultStateDir                = ".config"
	metricsDBFile                  = "metrics.db"
	thinkingCacheDBFile            = "thinking_cache.db"
	conversationStateFile          = "conversation_state.json"
	scheduledRecoveryStateFileName = "scheduled_recovery_state.json"
	autopilotDBFile                = "autopilot.db"
)

type cliOptions struct {
	Action     cliAction
	ConfigPath string
	StateDir   string
	LogDir     string
	BackupDir  string
}

type runtimePaths struct {
	ConfigPath                 string
	StateDir                   string
	MetricsDBPath              string
	ThinkingCacheDBPath        string
	ConversationStatePath      string
	ScheduledRecoveryStatePath string
	AutopilotDBPath            string
	PresetCacheDir             string
	LogDir                     string
	BackupDir                  string
}

func parseCLIArgs(args []string) (cliOptions, error) {
	opts := cliOptions{Action: cliActionRun}
	if len(args) == 1 && args[0] == "version" {
		opts.Action = cliActionVersion
		return opts, nil
	}

	fs := flag.NewFlagSet("yunqi", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	showHelp := false
	showVersion := false
	fs.BoolVar(&showHelp, "help", false, "显示帮助")
	fs.BoolVar(&showVersion, "version", false, "显示版本")
	fs.BoolVar(&showVersion, "v", false, "显示版本")
	fs.StringVar(&opts.ConfigPath, "config", "", "指定配置文件路径")
	fs.StringVar(&opts.StateDir, "statedir", "", "指定运行时状态目录")
	fs.StringVar(&opts.LogDir, "logdir", "", "指定日志目录")
	fs.StringVar(&opts.BackupDir, "backupdir", "", "指定配置备份目录")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			opts.Action = cliActionHelp
			return opts, nil
		}
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("未知参数: %s", strings.Join(fs.Args(), " "))
	}
	if showHelp {
		opts.Action = cliActionHelp
		return opts, nil
	}
	if showVersion {
		opts.Action = cliActionVersion
		return opts, nil
	}
	return opts, nil
}

func writeCLIHelp(out io.Writer) {
	_, _ = fmt.Fprint(out, `用法:
  yunqi [options]
  yunqi version

选项:
  --help, -h          显示帮助并退出
  --version, -v       显示版本信息并退出
  --config PATH       指定运行时配置文件路径，默认 .config/config.json
  --statedir DIR      指定运行时状态目录，默认 .config
  --logdir DIR        指定日志目录，优先级高于 LOG_DIR，默认 logs
                      使用 none 或 null 禁用日志文件写入（仅输出到控制台）
	  --backupdir DIR     指定配置备份目录，默认 配置文件同级目录下的 backups

示例:
  yunqi --config ~/.config/ccx/config.json --statedir ~/.local/state/ccx --logdir ~/.local/state/ccx/logs --backupdir ~/.local/state/ccx/backups
  yunqi --logdir none   # 禁用日志文件，仅输出到控制台

说明:
  --config 只改变配置文件位置。
  --statedir 会让 metrics.db、thinking_cache.db、conversation_state.json、scheduled_recovery_state.json
  写入指定目录；不指定时保持默认 .config。
  --logdir 只影响日志目录。使用 none 或 null 可禁用日志文件写入，适合 systemd/journald 等环境。
	  --backupdir 只影响配置备份目录，不指定时默认为配置文件同级目录下的 backups。
`)
}

func printVersion(out io.Writer) {
	_, _ = fmt.Fprintf(out, "yunqi %s\n", Version)
	if BuildTime != "unknown" {
		_, _ = fmt.Fprintf(out, "build time: %s\n", BuildTime)
	}
	if GitCommit != "unknown" {
		_, _ = fmt.Fprintf(out, "git commit: %s\n", GitCommit)
	}
}

func expandUserPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户主目录失败: %w", err)
		}
		if path == "~" {
			return filepath.Clean(homeDir), nil
		}
		return filepath.Clean(filepath.Join(homeDir, path[2:])), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("不支持 ~otheruser 形式: %s", path)
	}
	return filepath.Clean(path), nil
}

func resolveRuntimePaths(opts cliOptions, envCfg *config.EnvConfig) (runtimePaths, error) {
	configPath := defaultConfigPath
	if opts.ConfigPath != "" {
		expandedConfigPath, err := expandUserPath(opts.ConfigPath)
		if err != nil {
			return runtimePaths{}, fmt.Errorf("解析配置文件路径失败: %w", err)
		}
		configPath = expandedConfigPath
	}

	stateDir := defaultStateDir
	if opts.StateDir != "" {
		expandedStateDir, err := expandUserPath(opts.StateDir)
		if err != nil {
			return runtimePaths{}, fmt.Errorf("解析运行时状态目录失败: %w", err)
		}
		stateDir = expandedStateDir
	}

	logDir := envCfg.LogDir
	if opts.LogDir != "" {
		logDir = opts.LogDir
	}

	// 禁用日志文件 sentinel 归一化（none/null 不区分大小写）
	if logger.IsLogDisabled(logDir) {
		logDir = "none"
	} else if opts.LogDir != "" {
		// 非禁用值才需要展开路径
		expandedLogDir, err := expandUserPath(opts.LogDir)
		if err != nil {
			return runtimePaths{}, fmt.Errorf("解析日志目录失败: %w", err)
		}
		logDir = expandedLogDir
	}

	// 备份目录：CLI > 默认（配置文件同级 backups）
	backupDir := filepath.Join(filepath.Dir(configPath), "backups")
	if opts.BackupDir != "" {
		expandedBackupDir, err := expandUserPath(opts.BackupDir)
		if err != nil {
			return runtimePaths{}, fmt.Errorf("解析配置备份目录失败: %w", err)
		}
		backupDir = expandedBackupDir
	}

	return runtimePaths{
		ConfigPath:                 configPath,
		StateDir:                   stateDir,
		MetricsDBPath:              filepath.Join(stateDir, metricsDBFile),
		ThinkingCacheDBPath:        filepath.Join(stateDir, thinkingCacheDBFile),
		ConversationStatePath:      filepath.Join(stateDir, conversationStateFile),
		ScheduledRecoveryStatePath: filepath.Join(stateDir, scheduledRecoveryStateFileName),
		AutopilotDBPath:            filepath.Join(stateDir, autopilotDBFile),
		PresetCacheDir:             filepath.Join(stateDir, "presets"),
		LogDir:                     logDir,
		BackupDir:                  backupDir,
	}, nil
}
