package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// generateRandom 生成随机字符串
func generateRandom(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// sleepWithContext 带 context 的 sleep
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		time.Sleep(d)
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// retryDelay 计算重试延迟（指数退避）
func retryDelay(attempt int) time.Duration {
	base := 500 * time.Millisecond
	return base * time.Duration(1<<uint(attempt))
}
