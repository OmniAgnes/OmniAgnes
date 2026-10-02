package store

import (
	"testing"
	"time"
)

func mkSchedule(interval int, at string, lastRun, createdAt time.Time) *ScheduledTask {
	return &ScheduledTask{
		ID:           "sched0000001",
		Prompt:       "做点事",
		IntervalDays: interval,
		At:           at,
		Enabled:      true,
		LastRun:      lastRun,
		CreatedAt:    createdAt,
	}
}

func TestScheduleStatusDaily(t *testing.T) {
	// 创建于 10:00（时点 09:00 之后）→ 首个执行位为次日 09:00
	created := time.Date(2026, 10, 1, 10, 0, 0, 0, time.Local)

	// 从未执行：今天 09:00 未到 → 不执行，下次今天 09:00
	now := time.Date(2026, 10, 2, 8, 30, 0, 0, time.Local)
	due, next := ScheduleStatus(mkSchedule(1, "09:00", time.Time{}, created), now)
	if due || !next.Equal(time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("首次执行应在当日 09:00: due=%v next=%v", due, next)
	}

	// 从未执行且首个执行位在 24 小时内已错过（昨天 09:00 的，现在 10:00）→ 补执行
	now = time.Date(2026, 10, 2, 10, 0, 0, 0, time.Local)
	due, next = ScheduleStatus(mkSchedule(1, "09:00", time.Time{}, created), now)
	if !due {
		t.Fatal("24 小时内的错过应补执行")
	}
	if !next.Equal(time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("下次应为明天 09:00: %v", next)
	}

	// 从未执行且首个执行位已过期超过 24 小时 → 不补，直接排到下一个未来执行位
	now = time.Date(2026, 10, 4, 10, 0, 0, 0, time.Local)
	due, next = ScheduleStatus(mkSchedule(1, "09:00", time.Time{}, created), now)
	if due {
		t.Fatal("超过 24 小时的错过不应补执行")
	}
	if !next.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("下次应为 10-05 09:00: %v", next)
	}

	// 昨天正常执行过，今天 09:00 已过 → 补执行，下次明天 09:00
	last := time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)
	now = time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)
	due, next = ScheduleStatus(mkSchedule(1, "09:00", last, created), now)
	if !due {
		t.Fatal("今天 09:00 已过应补执行")
	}
	if !next.Equal(time.Date(2026, 10, 4, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("下次应为明天 09:00: %v", next)
	}

	// 今天 09:00 刚执行过（补执行晚了，14:00 才跑）→ 不重复执行，下次明天 09:00
	last = time.Date(2026, 10, 3, 14, 0, 0, 0, time.Local)
	now = time.Date(2026, 10, 3, 15, 0, 0, 0, time.Local)
	due, next = ScheduleStatus(mkSchedule(1, "09:00", last, created), now)
	if due {
		t.Fatal("已执行过今天的不应重复执行")
	}
	if !next.Equal(time.Date(2026, 10, 4, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("下次应为明天 09:00（不漂移到补执行时刻）: %v", next)
	}

	// 停用的任务不做判断由调用方负责；这里验证每 3 天的间隔
	last = time.Date(2026, 10, 1, 9, 0, 0, 0, time.Local)
	now = time.Date(2026, 10, 4, 10, 0, 0, 0, time.Local)
	due, next = ScheduleStatus(mkSchedule(3, "09:00", last, created), now)
	if !due {
		t.Fatal("每3天：第4天 10:00 应补执行")
	}
	if !next.Equal(time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("下次应为 10-07 09:00: %v", next)
	}
}
