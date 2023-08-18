package retry_config

import (
	"github.com/avast/retry-go"
	"time"
)

var RetryOptions = retry.MaxDelay(3 * time.Second)
