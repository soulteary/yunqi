// budget-ledger-init initializes a NEW Redis budget namespace explicitly.
// It does not read Yunqi runtime files or connect to any HTTP service.
package main

import (
	"context"
	"fmt"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/redisconn"
	"os"
	"time"
)

func main() {
	namespace := os.Getenv("BUDGET_REDIS_NAMESPACE")
	if namespace == "" {
		namespace = "yunqi"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := redisconn.New(os.Getenv("BUDGET_REDIS_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "无效的 Redis 配置")
		os.Exit(1)
	}
	defer client.Close()
	if err := entrybudget.InitializeRedis(ctx, client, namespace, os.Getenv("BUDGET_REDIS_EPOCH")); err != nil {
		fmt.Fprintln(os.Stderr, "预算命名空间初始化失败；已存在的状态必须恢复原账本和 epoch，不能重新初始化。")
		os.Exit(1)
	}
	fmt.Println("新预算命名空间已初始化；运行节点必须使用相同 namespace 与 epoch。")
}
