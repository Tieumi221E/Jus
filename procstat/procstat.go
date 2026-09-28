// Package procstat is what a process can say about itself as it ends, for
// the apps' benchmarks: a short-lived command's peak memory cannot be read
// from outside after it has gone.
package procstat

import (
	"fmt"
	"os"
)

// Report writes "<app>-stats peak-working-set <bytes>" on stderr when the
// variable env is set (JUSNOTE_STATS, JUSPLAY_STATS); call it just before
// exiting.
func Report(app, env string) {
	if os.Getenv(env) == "" {
		return
	}
	if peak := PeakWorkingSet(); peak > 0 {
		fmt.Fprintf(os.Stderr, "%s-stats peak-working-set %d\n", app, peak)
	}
}
