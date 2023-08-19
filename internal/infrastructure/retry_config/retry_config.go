package retry_config

import (
	"github.com/avast/retry-go"
	"time"
)

var RetryOptions = []retry.Option{
	retry.Delay(3 * time.Second),
	retry.Attempts(2),
}

// HandlerRetryOptions represents a retry loop that will attempt to create a release for up to two hours, which
// is the standard maintenance window for cloud hosted Octopus instances.
var HandlerRetryOptions = []retry.Option{
	retry.Attempts(6),
	retry.DelayType(func(n uint, _ error, config *retry.Config) time.Duration {
		if n == 0 {
			return 0
		}

		if n == 1 {
			return 1 * time.Minute
		}

		if n == 2 {
			return 5 * time.Minute
		}

		if n == 3 {
			return 10 * time.Minute
		}

		if n == 4 {
			return 15 * time.Minute
		}

		if n == 5 {
			return 30 * time.Minute
		}

		return 60 * time.Minute
	}),
}
