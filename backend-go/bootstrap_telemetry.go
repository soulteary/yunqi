package main

import (
	"context"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/telemetry"
	"log"
	"time"
)

func (a *application) initTelemetry() {
	a.monitoring = telemetry.NewRegistry()
	cfg, err := telemetry.OTLPConfigFromEnv()
	if err != nil {
		log.Fatal("[Telemetry-Init] OTLP配置无效")
	}
	if !cfg.Enabled {
		return
	}
	if err := a.monitoring.ConfigureTracing(context.Background(), cfg, httpclient.GetManager().GetStandardClient(2*time.Second, false)); err != nil {
		log.Fatal("[Telemetry-Init] OTLP初始化失败")
	}
}
