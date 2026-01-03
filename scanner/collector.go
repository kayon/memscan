package scanner

// CollectorFunc
// return false to terminate scanning
type CollectorFunc func(offset int) bool
