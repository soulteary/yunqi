package main

import (
	"log"
	"time"

	"github.com/soulteary/yunqi/internal/scheduler"
)

func (a *application) startScheduledRecovery() {
	a.scheduledRecoveryStop = make(chan struct{})
	go func() {
		runScheduledRecovery := func(now time.Time, missedSlot time.Time) bool {
			effectiveTime := now.UTC()
			if !missedSlot.IsZero() {
				effectiveTime = missedSlot.UTC()
				log.Printf("[Scheduler-Recovery] 检测到错过 UTC 恢复槽位 %s，立即补跑", missedSlot.Format(time.RFC3339))
			}
			results, err := a.channelScheduler.RunScheduledRecoveries(effectiveTime)
			if err != nil {
				log.Printf("[Scheduler-Recovery] 警告: 自动恢复执行失败: %v", err)
				return false
			}
			if len(results) == 0 {
				log.Printf("[Scheduler-Recovery] UTC 自动恢复完成，本轮无可恢复 key")
				return true
			}
			restoredKeys := 0
			restoredKeyModels := 0
			activatedChannels := 0
			for _, result := range results {
				restoredKeys += len(result.RestoredKeys)
				restoredKeyModels += len(result.RestoredKeyModels)
				if result.ActivatedChannel {
					activatedChannels++
				}
			}
			log.Printf("[Scheduler-Recovery] UTC 自动恢复完成：恢复 %d 个 key，%d 个 (key,模型) 组合，激活 %d 个渠道", restoredKeys, restoredKeyModels, activatedChannels)
			return true
		}
		runDueRecovery := func(now time.Time) {
			results, err := a.channelScheduler.RunDueRecoveries(now.UTC())
			if err != nil {
				log.Printf("[Scheduler-Recovery] 警告: 到期恢复执行失败: %v", err)
				return
			}
			if len(results) == 0 {
				return
			}
			restoredKeys := 0
			restoredKeyModels := 0
			activatedChannels := 0
			for _, result := range results {
				restoredKeys += len(result.RestoredKeys)
				restoredKeyModels += len(result.RestoredKeyModels)
				if result.ActivatedChannel {
					activatedChannels++
				}
			}
			log.Printf("[Scheduler-Recovery] 到期恢复完成：恢复 %d 个 key，%d 个 (key,模型) 组合，激活 %d 个渠道", restoredKeys, restoredKeyModels, activatedChannels)
		}

		recordRecoveryCheck := func(checkedAt time.Time) {
			if err := saveScheduledRecoveryLastCheck(a.paths.ScheduledRecoveryStatePath, checkedAt); err != nil {
				log.Printf("[Scheduler-Recovery] 警告: 持久化恢复检查时间失败: %v", err)
			}
		}

		lastRecoveryCheck, err := loadScheduledRecoveryLastCheck(a.paths.ScheduledRecoveryStatePath)
		if err != nil {
			log.Printf("[Scheduler-Recovery] 警告: 读取恢复检查时间失败: %v", err)
			lastRecoveryCheck = time.Time{}
		}
		commitRecoveryCheck := func(checkedAt time.Time, attempted bool, succeeded bool) {
			if attempted && !succeeded {
				log.Printf("[Scheduler-Recovery] 警告: 本轮恢复失败，保留检查点 %s 以便后续重试", lastRecoveryCheck.Format(time.RFC3339))
				return
			}
			lastRecoveryCheck = checkedAt
			recordRecoveryCheck(lastRecoveryCheck)
		}

		startupNow := time.Now().UTC()
		runDueRecovery(startupNow)
		if !lastRecoveryCheck.IsZero() {
			if missedSlot, ok := scheduler.MissedScheduledRecoveryTimeUTC(lastRecoveryCheck, startupNow); ok {
				commitRecoveryCheck(startupNow, true, runScheduledRecovery(startupNow, missedSlot))
			} else {
				commitRecoveryCheck(startupNow, false, true)
			}
		} else {
			commitRecoveryCheck(startupNow, false, true)
		}

		recoveryFallbackTicker := time.NewTicker(1 * time.Minute)
		defer recoveryFallbackTicker.Stop()

		for {
			next := scheduler.NextScheduledRecoveryTimeUTC(time.Now())
			wait := time.Until(next)
			if wait < 0 {
				wait = 0
			}
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
				now := time.Now().UTC()
				scheduledAt := next.UTC()
				if now.After(scheduledAt.Add(time.Second)) {
					if missedSlot, ok := scheduler.MissedScheduledRecoveryTimeUTC(lastRecoveryCheck, now); ok {
						commitRecoveryCheck(now, true, runScheduledRecovery(now, missedSlot))
					} else {
						commitRecoveryCheck(now, true, runScheduledRecovery(scheduledAt, time.Time{}))
					}
				} else {
					commitRecoveryCheck(now, true, runScheduledRecovery(scheduledAt, time.Time{}))
				}
			case tickAt := <-recoveryFallbackTicker.C:
				now := tickAt.UTC()
				runDueRecovery(now)
				if missedSlot, ok := scheduler.MissedScheduledRecoveryTimeUTC(lastRecoveryCheck, now); ok {
					commitRecoveryCheck(now, true, runScheduledRecovery(now, missedSlot))
				} else {
					commitRecoveryCheck(now, false, true)
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-a.scheduledRecoveryStop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}
	}()
}
