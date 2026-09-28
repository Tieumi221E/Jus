// Package conform is the Jus conformance test: run against
// an app's program, it checks what makes the apps one series rather than a
// promise — the capability list (`help -json`), the command line built from
// it (spelling, exit codes, the -json error), the manifest and its
// footprint. `jus conform <exe>` runs it; each app's tests
// run it on its own build.
package conform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Tieumi221E/Jus/apps"
)

// Check is one result.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Report is every check of one program.
type Report struct {
	Exe    string  `json:"exe"`
	App    string  `json:"app"`
	Checks []Check `json:"checks"`
	Ms     int64   `json:"ms"`
}

// OK reports whether every check passed.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return len(r.Checks) > 0
}

type out struct {
	code           int
	stdout, stderr []byte
}

// run runs exe with args, in dir, with env added.
func run(ctx context.Context, exe, dir string, env []string, args ...string) (out, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return out{ee.ExitCode(), o.Bytes(), e.Bytes()}, nil
	case err != nil:
		return out{}, err
	}
	return out{0, o.Bytes(), e.Bytes()}, nil
}

type helpDoc struct {
	App      string            `json:"app"`
	Version  string            `json:"version"`
	Exit     map[string]string `json:"exit"`
	Commands []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
		Writes  bool   `json:"writes"`
		Confirm bool   `json:"confirm"`
		Window  bool   `json:"window"`
		Params  []struct {
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			Required   bool   `json:"required"`
			Positional bool   `json:"positional"`
			Stdin      bool   `json:"stdin"`
		} `json:"params"`
	} `json:"commands"`
}

var idRE = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)?$`)

// Run checks the program exe. dir is a scratch folder the checks may run
// in (they write nothing on purpose); env is added to the program's
// environment (e.g. its data folder, so a person's own window is not
// told anything).
func Run(ctx context.Context, exe, dir string, env []string) (rep Report) {
	start := time.Now()
	rep = Report{Exe: exe}
	check := func(name string, ok bool, format string, a ...any) {
		rep.Checks = append(rep.Checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(format, a...)})
	}
	defer func() { rep.Ms = time.Since(start).Milliseconds() }()

	// The capability list.
	h, err := run(ctx, exe, dir, env, "help", "-json")
	var help helpDoc
	if err == nil && h.code == 0 {
		err = json.Unmarshal(h.stdout, &help)
	}
	check("help -json is the capability list", err == nil && h.code == 0 && help.App != "" && len(help.Commands) > 0,
		"exit %d, %d commands, %v", h.code, len(help.Commands), err)
	if help.App == "" {
		return rep
	}
	rep.App = help.App
	check("its name is jus<something>", strings.HasPrefix(help.App, "jus") && len(help.App) > 3, "%q", help.App)
	check("exit codes are the series' (0 ok, 1 failed, 2 usage, 3 conflict)",
		help.Exit["0"] != "" && help.Exit["1"] != "" && help.Exit["2"] != "" && help.Exit["3"] != "", "%v", keys(help.Exit))

	seen := map[string]bool{}
	var bad []string
	for _, c := range help.Commands {
		if !idRE.MatchString(c.ID) || seen[c.ID] || c.Summary == "" || c.Name != strings.ReplaceAll(c.ID, ".", " ") {
			bad = append(bad, c.ID)
		}
		seen[c.ID] = true
	}
	check("every capability has a unique group.verb id, its spelling and a summary", len(bad) == 0, "bad: %v", bad)
	check("it has a manifest capability", seen["manifest"], "")

	// Its version.
	v, err := run(ctx, exe, dir, env, "version")
	check("version prints the name and version", err == nil && v.code == 0 && strings.TrimSpace(string(v.stdout)) == help.App+" "+help.Version,
		"%q, want %q", strings.TrimSpace(string(v.stdout)), help.App+" "+help.Version)

	// The command line: an unknown command, a missing parameter.
	u, err := run(ctx, exe, dir, env, "no-such-command-xyz")
	check("an unknown command exits 2", err == nil && u.code == 2, "exit %d", u.code)
	var target string
	for _, c := range help.Commands {
		if c.Window || c.Writes {
			continue
		}
		for _, p := range c.Params {
			if p.Required && !p.Stdin {
				target = c.Name
			}
		}
		if target != "" {
			break
		}
	}
	if target != "" {
		m, err := run(ctx, exe, dir, env, append(strings.Fields(target), "-json")...)
		var e struct {
			Error string `json:"error"`
			Kind  string `json:"kind"`
			Code  int    `json:"code"`
		}
		jerr := json.Unmarshal(bytes.TrimSpace(m.stderr), &e)
		check("a missing parameter exits 2, and -json says so on stderr as {error, kind, code}",
			err == nil && m.code == 2 && jerr == nil && e.Kind == "usage" && e.Code == 2 && e.Error != "",
			"%s: exit %d, stderr %s", target, m.code, strings.TrimSpace(string(m.stderr)))
	}

	// The manifest and its footprint.
	man, err := apps.Ask(ctx, exe)
	check("manifest -json answers", err == nil, "%v", err)
	if err != nil {
		return rep
	}
	check("the manifest is the same app and version as help", man.ID == help.App && man.Version == help.Version,
		"manifest %s %s, help %s %s", man.ID, man.Version, help.App, help.Version)
	var links []string
	for _, l := range man.Links {
		if !strings.HasPrefix(l, "jus://") || !strings.HasSuffix(l, "/") {
			links = append(links, l)
		}
	}
	check("its links are jus://<name>/ prefixes", len(links) == 0, "bad: %v", links)
	var formats []string
	for _, f := range man.Formats {
		if f.Name == "" || f.Version < 1 {
			formats = append(formats, f.Name)
		}
	}
	check("every data format it writes has a version", len(formats) == 0, "bad: %v", formats)
	var places []string
	hasProgram, hasData := false, false
	for _, p := range man.Footprint {
		switch p.Kind {
		case apps.Program:
			hasProgram = true
		case apps.Data:
			hasData = true
		case apps.Cache, apps.User:
		default:
			places = append(places, p.Path+" ("+p.Kind+")")
			continue
		}
		if !filepath.IsAbs(p.Path) || p.What == "" {
			places = append(places, p.Path)
		}
	}
	check("the footprint names every place as an absolute path, with a kind and what it is", len(places) == 0, "bad: %v", places)
	check("the footprint has its program and its data folder", hasProgram && hasData, "program %v, data %v", hasProgram, hasData)
	plan := apps.PlanUninstall(man)
	check("jus uninstall would take every place it names as the app's own", len(plan.Refused) == 0, "refused: %v", plan.Refused)
	return rep
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
