package truthsensor

import "context"

type Event struct {
	TimestampNS uint64
	PID         uint32
	ParentPID   uint32
	Type        uint32
	Command     string
	Detail      string
}
type Sensor interface {
	Run(context.Context, func(Event) error) error
	Close() error
}
