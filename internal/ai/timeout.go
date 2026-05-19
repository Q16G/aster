package ai

import "time"

func AttemptTimeoutForAttempt(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	if attempt < 0 {
		attempt = 0
	}

	timeoutCap := base * 4
	if timeoutCap < 180*time.Second {
		timeoutCap = 180 * time.Second
	}

	timeout := base
	for i := 0; i < attempt; i++ {
		if timeout >= timeoutCap {
			return timeoutCap
		}
		if timeout > timeoutCap/2 {
			timeout = timeoutCap
		} else {
			timeout *= 2
		}
	}
	if timeout > timeoutCap {
		timeout = timeoutCap
	}
	return timeout
}
