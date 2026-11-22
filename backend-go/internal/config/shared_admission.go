package config

import (
	"context"
	"time"
)

// SharedProxyAdmission keeps runtime networking outside the configuration model.
// The returned cleanup runs when the logical handler finishes; bound response
// bodies retain the lease independently until actual upstream work ends.
type SharedProxyAdmission interface {
	AcquireContext(context.Context, string, int, int) (context.Context, func(), time.Duration, error)
	Close() error
}

// SharedRoutingAdmission applies configured channel capacity across nodes.
type SharedRoutingAdmission interface {
	AcquireWindowContext(context.Context, string, int, int, time.Duration) (context.Context, func(), time.Duration, error)
	Close() error
}
