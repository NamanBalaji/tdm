package download

// Status represents the state of a download in its lifecycle.
type Status int32

const (
	Pending   Status = iota // Just created, not yet scheduled
	Active                  // Currently downloading
	Paused                  // Stopped by user, can resume
	Completed               // Successfully finished
	Failed                  // Stopped due to error
	Queued                  // Waiting for a slot in the scheduler
	Cancelled               // Stopped by user, cannot resume
)

func (s Status) IsTerminal() bool {
	return s == Completed || s == Failed || s == Cancelled
}

func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Active:
		return "active"
	case Paused:
		return "paused"
	case Completed:
		return "completed"
	case Failed:
		return "failed"
	case Queued:
		return "queued"
	case Cancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}
