package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanUninstall(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, dir bool) string {
		p := filepath.Join(root, rel)
		if dir {
			os.MkdirAll(p, 0o755)
		} else {
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("x"), 0o644)
		}
		return p
	}
	exe := mk("Jusnote-0.3.0/"+exeName("jusnote"), false)
	data := mk("Jusnote-0.3.0/jusnote-data", true)
	models := mk("Jusnote-0.3.0/components/ai", true) // inside the app's own folder
	cache := mk("notes/.jusnote/cache", true)
	types := mk("notes/.jusnote/types", true)
	other := mk("notes/important.md", false)
	otherExe := mk("tools/"+exeName("other"), false)
	m := Manifest{ID: "jusnote", Footprint: []Place{
		{Path: exe, Kind: Program}, {Path: data, Kind: Data}, {Path: cache, Kind: Cache}, {Path: models, Kind: Cache},
		{Path: types, Kind: User},
		{Path: other, Kind: Cache},                                      // wrong: not inside .jusnote
		{Path: otherExe, Kind: Program},                                 // wrong: not jusnote.exe
		{Path: root, Kind: Data},                                        // wrong: no "jusnote" in its name
		{Path: filepath.Join(root, "gone", "jusnote-data"), Kind: Data}, // not there
	}}
	p := PlanUninstall(m)
	if len(p.Remove) != 4 || len(p.Keep) != 1 || len(p.Refused) != 3 {
		t.Fatalf("plan: remove %v keep %v refused %v", p.Remove, p.Keep, p.Refused)
	}
	r := Uninstall(p)
	if len(r.Removed) != 5 || len(r.Left) != 0 { // and the emptied Jusnote-0.3.0 folder
		t.Fatalf("result: %+v", r)
	}
	for _, gone := range []string{exe, data, cache, models} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still there", gone)
		}
	}
	for _, kept := range []string{types, other, otherExe, root} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was removed", kept)
		}
	}
}
