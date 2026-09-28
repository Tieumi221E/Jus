//go:build !windows

package procstat

// PeakWorkingSet is not measured here yet.
func PeakWorkingSet() uint64 { return 0 }
