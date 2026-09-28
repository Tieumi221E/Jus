// Package apps is how the Jus apps know of each other: each answers `<app> manifest -json` with what it is — its links,
// its data formats, and every place it writes (its footprint) — and finds
// the others where the release zips put them: beside it, in a folder
// beside its own, or on PATH. Nothing is registered anywhere; an app that
// is not there is simply not found, and the finder says so.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Tieumi221E/Jus/capreg"
)

// Manifest is what an app says about itself.
type Manifest struct {
	ID      string `json:"id"` // "jusnote"
	Name    string `json:"name"`
	Version string `json:"version"`
	Exe     string `json:"exe"`
	// Links are the jus:// prefixes it opens ("jus://note/").
	Links   []string `json:"links,omitempty"`
	Formats []Format `json:"formats,omitempty"`
	// Footprint is every place it writes, as it is on this computer now.
	Footprint []Place `json:"footprint"`
}

// Format is one of its open data formats and the version it writes.
type Format struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Doc     string `json:"doc,omitempty"`
}

// Kinds of place.
const (
	Program = "program" // the app itself
	Data    = "data"    // its own settings and records, outside the user's folders
	Cache   = "cache"   // what it can make again
	User    = "user"    // the user's: kept by an uninstall, only listed
)

// Place is one place an app writes.
type Place struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	What string `json:"what"`
}

// RegisterManifest adds the manifest capability to r; m is asked each time
// (the footprint is what is on disk now).
func RegisterManifest(r *capreg.Registry, m func() Manifest) {
	r.Add(capreg.Cap{ID: "manifest", Summary: "what this app is, for jus and the other Jus apps: its links, data formats, and every place it writes (jus uninstall uses it)",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			man := m()
			if man.Footprint == nil {
				man.Footprint = []Place{}
			}
			return man, nil
		}})
}

// Exe is how the app names its program: the executable, "" if unknown.
func Exe() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if q, err := filepath.EvalSymlinks(p); err == nil {
		p = q
	}
	return p
}

func exeName(id string) string {
	if runtime.GOOS == "windows" {
		return id + ".exe"
	}
	return id
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Find is the program of app id ("jusplay"), or "": JUS_<ID>_EXE, beside
// this program, in a folder beside this one's whose name has the id
// (…\Jusplay-0.4.0-windows-x64\), or on PATH.
func Find(id string) string {
	if p := os.Getenv("JUS_" + strings.ToUpper(id) + "_EXE"); p != "" && isFile(p) {
		return p
	}
	file := exeName(id)
	if exe := Exe(); exe != "" {
		dir := filepath.Dir(exe)
		if p := filepath.Join(dir, file); isFile(p) {
			return p
		}
		if sibs, err := filepath.Glob(filepath.Join(filepath.Dir(dir), "*", file)); err == nil {
			for _, p := range sibs {
				if strings.Contains(strings.ToLower(filepath.Base(filepath.Dir(p))), id) {
					return p
				}
			}
		}
	}
	if p, err := exec.LookPath(id); err == nil {
		return p
	}
	return ""
}

// candidates are the programs that may be Jus apps: jus*.exe where Find
// looks, and the JUS_*_EXE variables.
func candidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		base := strings.ToLower(filepath.Base(p))
		if base == exeName("jus") || !strings.HasPrefix(base, "jus") || seen[strings.ToLower(p)] || !isFile(p) {
			return
		}
		if runtime.GOOS == "windows" && !strings.HasSuffix(base, ".exe") {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(k), "JUS_") && strings.HasSuffix(strings.ToUpper(k), "_EXE") {
			add(v)
		}
	}
	glob := func(dir string) {
		ps, _ := filepath.Glob(filepath.Join(dir, "jus*"))
		for _, p := range ps {
			add(p)
		}
	}
	if exe := Exe(); exe != "" {
		dir := filepath.Dir(exe)
		glob(dir)
		if sibs, err := os.ReadDir(filepath.Dir(dir)); err == nil {
			for _, s := range sibs {
				if s.IsDir() && strings.HasPrefix(strings.ToLower(s.Name()), "jus") {
					glob(filepath.Join(filepath.Dir(dir), s.Name()))
				}
			}
		}
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d != "" {
			glob(d)
		}
	}
	return out
}

// Call runs a capability of another app's program (its command line,
// -json), without a window flashing up: the result, or its error — the
// message of the app's {error, kind, code} when it printed one.
func Call(ctx context.Context, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, append(args, "-json")...)
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(ee.Stderr, &e) == nil && e.Error != "" {
				return nil, errors.New(e.Error)
			}
			return nil, errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// Ask is the manifest of the program at exe (it is run: `exe manifest -json`).
func Ask(ctx context.Context, exe string) (Manifest, error) {
	out, err := Call(ctx, exe, "manifest")
	if err != nil {
		return Manifest{}, fmt.Errorf("%s manifest: %w", filepath.Base(exe), err)
	}
	var m Manifest
	if err := json.Unmarshal(out, &m); err != nil {
		return Manifest{}, fmt.Errorf("%s manifest: not JSON: %w", filepath.Base(exe), err)
	}
	if m.ID == "" {
		return Manifest{}, fmt.Errorf("%s manifest: no id", filepath.Base(exe))
	}
	if m.Exe == "" {
		m.Exe = exe
	}
	return m, nil
}

// Found is one program looked at by All: its manifest, or why there is none.
type Found struct {
	Exe      string    `json:"exe"`
	Manifest *Manifest `json:"manifest,omitempty"`
	Problem  string    `json:"problem,omitempty"`
}

// All is every Jus app found on this computer, by id (the first found of
// each), and the programs that looked like one but did not answer.
func All(ctx context.Context) (apps []Manifest, others []Found) {
	byID := map[string]bool{}
	for _, exe := range candidates() {
		m, err := Ask(ctx, exe)
		if err != nil {
			others = append(others, Found{Exe: exe, Problem: err.Error()})
			continue
		}
		if byID[m.ID] {
			continue
		}
		byID[m.ID] = true
		apps = append(apps, m)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	return apps, others
}
