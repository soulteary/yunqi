package gemini

import (
	"os"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
)

// 协议测试的兼容性学习只存内存，保持到进程退出，避免污染源码目录或复用上次测试的状态。
func TestMain(m *testing.M) {
	common.SwapChannelCompatCacheForTest(config.NewChannelCompatCache())
	os.Exit(m.Run())
}
