package apps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Plan is what uninstalling an app does: what goes, what stays (the
// user's), and what its manifest named that jus will not touch.
type Plan struct {
	App     string  `json:"app"`
	Remove  []Place `json:"remove"`
	Keep    []Place `json:"keep"`
	Refused []Place `json:"refused,omitempty"` // named, but not a place of this app's by its look
}

// PlanUninstall is the plan for m. A place goes only if it looks like the
// app's own — its program is <id>.exe, its data folder has the id in its
// name, a cache is inside a .<id-without-jus> folder (.jusnote, .jusplay)
// — so a wrong manifest cannot make jus delete anything else.
func PlanUninstall(m Manifest) Plan {
	p := Plan{App: m.ID, Remove: []Place{}, Keep: []Place{}}
	// The folder the program is in, when it is the app's own (named for it:
	// …\Jusplay-0.4.0-windows-x64\): what is inside is the app's too.
	var homes []string
	for _, pl := range m.Footprint {
		if pl.Kind == Program && owns(m.ID, pl, nil) {
			if dir := filepath.Dir(filepath.Clean(pl.Path)); strings.Contains(strings.ToLower(filepath.Base(dir)), strings.ToLower(m.ID)) {
				homes = append(homes, dir)
			}
		}
	}
	for _, pl := range m.Footprint {
		if pl.Kind == User {
			p.Keep = append(p.Keep, pl)
			continue
		}
		if !owns(m.ID, pl, homes) {
			p.Refused = append(p.Refused, pl)
			continue
		}
		if _, err := os.Lstat(pl.Path); err != nil {
			continue // not there: nothing to remove
		}
		p.Remove = append(p.Remove, pl)
	}
	return p
}

func owns(id string, pl Place, homes []string) bool {
	path := filepath.Clean(pl.Path)
	if !filepath.IsAbs(path) || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && strings.EqualFold(path, filepath.Clean(home)) {
		return false
	}
	if pl.Kind == Data || pl.Kind == Cache {
		for _, h := range homes {
			if rel, err := filepath.Rel(h, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
	}
	base := strings.ToLower(filepath.Base(path))
	id = strings.ToLower(id)
	switch pl.Kind {
	case Program:
		return base == exeName(id)
	case Data:
		return strings.Contains(base, id)
	case Cache:
		vault := "." + id // .jusnote
		for _, seg := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
			if seg == vault {
				return true
			}
		}
		return strings.Contains(base, id)
	}
	return false
}

// Result is what an uninstall did.
type Result struct {
	Removed []string `json:"removed"`
	// Left are places that could not be removed (in use, no permission).
	Left []string `json:"left,omitempty"`
	Kept []Place  `json:"kept"`
}

// Uninstall carries out p and checks that what it removed is gone.
func Uninstall(p Plan) Result {
	r := Result{Removed: []string{}, Kept: p.Keep}
	for _, pl := range p.Remove {
		err := os.RemoveAll(pl.Path)
		if _, st := os.Lstat(pl.Path); st == nil || err != nil {
			r.Left = append(r.Left, fmt.Sprintf("%s (%v)", pl.Path, orGone(err)))
			continue
		}
		r.Removed = append(r.Removed, pl.Path)
	}
	// The release folder the program came in: the folders emptied in it,
	// and it, when nothing else is left.
	for _, pl := range p.Remove {
		if dir := filepath.Dir(pl.Path); pl.Kind == Program && strings.Contains(strings.ToLower(filepath.Base(dir)), strings.ToLower(p.App)) {
			var dirs []string
			filepath.WalkDir(dir, func(q string, d fs.DirEntry, err error) error {
				if err == nil && d.IsDir() {
					dirs = append(dirs, q)
				}
				return nil
			})
			sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) }) // deepest first
			for _, d := range dirs {
				if os.Remove(d) == nil && d == dir { // Remove takes only empty folders
					r.Removed = append(r.Removed, dir)
				}
			}
		}
	}
	return r
}

func orGone(err error) any {
	if err == nil {
		return "still there"
	}
	return err
}
