package shared

import "time"

func timeoutProbe() <-chan time.Time { return time.After(5 * time.Second) }
