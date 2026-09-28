// Package instance lets an app's command line find its running window and
// hand calls to it: the window announces itself in the
// app's data folder (instance.json: process, page URL with its random
// token, version); the command line reads it and checks that the window
// answers. The file is readable by the user only (0600): the URL is the
// window's key.
package instance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Tieumi221E/Jus/capreg"
)

// Info is what a running window announces.
type Info struct {
	App     string    `json:"app"`
	PID     int       `json:"pid"`
	URL     string    `json:"url"` // the page URL, with the window's random token, ending in "/"
	Version string    `json:"version"`
	Started time.Time `json:"started"`
}

// File is where the window of the data folder dir announces itself.
func File(dir string) string { return filepath.Join(dir, "instance.json") }

// Announce writes the announcement for the window serving at url and
// returns the function that removes it again (only while it is still this
// process's: a newer window may have announced itself since).
func Announce(dir, app, version, url string) (remove func()) {
	b, _ := json.Marshal(Info{App: app, PID: os.Getpid(), URL: url, Version: version, Started: time.Now()})
	p := File(dir)
	tmp := p + ".partial"
	if os.WriteFile(tmp, b, 0o600) != nil || os.Rename(tmp, p) != nil {
		os.Remove(tmp)
		return func() {}
	}
	return func() {
		if in, ok := read(dir); ok && in.PID == os.Getpid() {
			os.Remove(p)
		}
	}
}

func read(dir string) (Info, bool) {
	b, err := os.ReadFile(File(dir))
	if err != nil {
		return Info{}, false
	}
	var in Info
	if json.Unmarshal(b, &in) != nil || in.URL == "" {
		return Info{}, false
	}
	return in, true
}

// Find is the running window of data folder dir, if one answers.
func Find(dir string) (capreg.Remote, Info, bool) {
	in, ok := read(dir)
	if !ok {
		return capreg.Remote{}, Info{}, false
	}
	rm := capreg.Remote{Base: in.URL}
	if !rm.Alive(context.Background()) {
		return capreg.Remote{}, in, false
	}
	return rm, in, true
}
