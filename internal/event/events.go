package event

import "time"

type EventType uint8

const (
	TaskCreated EventType = iota
)

type Event struct {
	Type      EventType
	Data      any
	Timestamp time.Time
}
