package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelperSkill is the skill's program: this test binary run again.
func TestHelperSkill(t *testing.T) {
	if os.Getenv("JUSNOTE_SKILL_HELPER") != "1" {
		t.Skip("the skill program, run by the other tests")
	}
	out := os.Getenv("JUS_OUT")
	args := os.Args[len(os.Args)-2:]
	os.WriteFile(filepath.Join(out, "report.md"), []byte("# report\n"+strings.Join(args, " ")+"\n"), 0o644)
	fmt.Println("made report for", os.Getenv("JUS_HARNESS"))
	os.Exit(3)
}

func makeSkill(t *testing.T, vault, name string, run []string) {
	t.Helper()
	dir := filepath.Join(Dir(vault), name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: \"makes a report\"\n---\n\nHow to use it.\n"), 0o644)
	if run != nil {
		b, _ := json.Marshal(map[string]any{"run": run})
		os.WriteFile(filepath.Join(dir, "skill.json"), b, 0o644)
	}
}

func TestListTrustRun(t *testing.T) {
	t.Setenv("JUSNOTE_SKILL_HELPER", "1")
	root := t.TempDir()
	vault := filepath.Join(root, ".jusnote")
	trust := OpenTrust(t.TempDir())
	makeSkill(t, vault, "report", []string{os.Args[0], "-test.run=^TestHelperSkill$", "--", "{notebook}", "x"})
	makeSkill(t, vault, "guide", nil)
	os.MkdirAll(filepath.Join(Dir(vault), "notaskill"), 0o755)

	ss, err := List(vault, trust)
	if err != nil || len(ss) != 2 || ss[0].Name != "guide" || ss[1].Name != "report" {
		t.Fatalf("List = %+v, %v", ss, err)
	}
	if ss[1].Description != "makes a report" || ss[1].Trusted || ss[0].Run != nil {
		t.Fatalf("skills read wrong: %+v", ss)
	}
	if _, err := Run(context.Background(), ss[1], root, nil, nil); err == nil {
		t.Fatal("an unconfirmed skill ran")
	}
	if _, err := Run(context.Background(), ss[0], root, nil, nil); err == nil {
		t.Fatal("an instructions-only skill ran")
	}

	trust.Add(vault, "report", ss[1].Hash)
	s, _ := Load(vault, "report", trust)
	if !s.Trusted {
		t.Fatal("confirmed skill not trusted")
	}
	res, err := Run(context.Background(), s, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Exit != 3 || !strings.Contains(res.Tail, "made report for skill/report") {
		t.Fatalf("result: %+v", res)
	}
	if len(res.Files) != 1 || res.Files[0] != "report.md" {
		t.Fatalf("output files: %v", res.Files)
	}
	b, _ := os.ReadFile(filepath.Join(OutDir(vault, "report"), "report.md"))
	if !strings.Contains(string(b), root+" x") {
		t.Fatalf("placeholders not filled: %q", b)
	}

	// Its files change: it needs confirming again. A __pycache__ does not count.
	os.MkdirAll(filepath.Join(s.Dir, "__pycache__"), 0o755)
	os.WriteFile(filepath.Join(s.Dir, "__pycache__", "m.pyc"), []byte("x"), 0o644)
	if s2, _ := Load(vault, "report", trust); !s2.Trusted {
		t.Fatal("a __pycache__ made the skill untrusted")
	}
	os.WriteFile(filepath.Join(s.Dir, "helper.py"), []byte("print(1)\n"), 0o644)
	if s2, _ := Load(vault, "report", trust); s2.Trusted {
		t.Fatal("a changed skill kept its confirmation")
	}
}

func TestLoadRejectsPaths(t *testing.T) {
	vault := t.TempDir()
	for _, n := range []string{"", ".", "..", "../x", `a\b`, "c:x"} {
		if _, err := Load(vault, n, nil); err == nil {
			t.Errorf("Load(%q) worked", n)
		}
	}
}
