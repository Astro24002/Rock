package testkit

import "time"

type FixedClock struct {
	Time time.Time
}

func (c FixedClock) Now() time.Time { return c.Time }
