package conflict

import (
	"context"
	"time"
)

type Logf func(format string, args ...any)

// StartScheduledScan اسکن دوره‌ای آرشیو را اجرا می‌کند.
func StartScheduledScan(ctx context.Context, store *Store, every time.Duration, logf Logf) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report := ScanArchive(store)
			if logf != nil {
				logf("scheduled archive scan completed: %d relationships", report.Summary["total"])
			}
		}
	}
}
