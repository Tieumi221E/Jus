package procstat

import (
	"runtime"
	"testing"
)

func TestPeakWorkingSet(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("measured on Windows only")
	}
	if p := PeakWorkingSet(); p < 1<<20 {
		t.Fatalf("peak working set %d bytes: not measured", p)
	}
}
