package progress

type Progress interface {
	GetTotalSize() int64
	GetDownloaded() int64
	GetPercentage() float64
	GetSpeedBPS() int64
	GetETA() string
}
