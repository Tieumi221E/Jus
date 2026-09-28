// Package skills runs a folder's own tools: folders
// under <vault>/skills/<name> (.jusnote/skills, .jusplay/skills, …), each
// with a SKILL.md (what it does, for people and agents alike: the usual
// front matter, name and description) and, when it runs, a skill.json
// saying how:
//
//	{"run": ["python", "{skill}/make.py", "{out}"], "timeout": 600}
//
// {skill} is the skill's folder, {out} its output folder (<vault>/out/<name>,
// not history), {root} the folder the vault is in ({notebook} is the same,
// as Jusnote first called it). The command runs in that folder with
// JUS_ROOT, JUS_OUT and JUS_SKILL set, and JUS_HARNESS=skill/<name>, so
// what it writes through a Jus app is marked as the skill's; the app adds
// its own variables (Run's env).
//
// A skill is code that came with the folder — maybe from someone else's
// copy — so it runs only once the user has seen its command and said yes,
// and again after any of its files change (the confirmation is kept in
// the app's own folder, by the skill's hash, not in the folder).
package skills

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Skill is one of a notebook's skills.
type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Dir         string   `json:"dir"`
	Run         []string `json:"run,omitempty"` // nil: only instructions (for agents), nothing to run
	Timeout     int      `json:"timeout,omitempty"`
	Hash        string   `json:"hash"`
	// Trusted: the user said yes to this version of it.
	Trusted bool   `json:"trusted"`
	Out     string `json:"out"`
	Problem string `json:"problem,omitempty"` // why it cannot run (skill.json unreadable)
}

// Dir is where a notebook's skills are.
func Dir(vault string) string { return filepath.Join(vault, "skills") }

// OutDir is where skill name writes its results.
func OutDir(vault, name string) string { return filepath.Join(vault, "out", name) }

// List is the notebook's skills, by name; trust is the app's record.
func List(vault string, trust *Trust) ([]Skill, error) {
	ents, err := os.ReadDir(Dir(vault))
	if errors.Is(err, fs.ErrNotExist) {
		return []Skill{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Skill{}
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s, err := Load(vault, e.Name(), trust)
		if err != nil {
			continue // a folder without SKILL.md is not a skill
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load reads skill name.
func Load(vault, name string, trust *Trust) (Skill, error) {
	if name == "" || strings.ContainsAny(name, `/\:`) || name == "." || name == ".." {
		return Skill{}, fmt.Errorf("no skill %q", name)
	}
	dir := filepath.Join(Dir(vault), name)
	md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, fmt.Errorf("no skill %s (%s has no SKILL.md)", name, dir)
	}
	s := Skill{Name: name, Dir: dir, Out: OutDir(vault, name)}
	s.Description = frontMatter(md)["description"]
	if b, err := os.ReadFile(filepath.Join(dir, "skill.json")); err == nil {
		var f struct {
			Run     []string `json:"run"`
			Timeout int      `json:"timeout"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			s.Problem = "skill.json: " + err.Error()
		} else if len(f.Run) == 0 {
			s.Problem = `skill.json has no "run"`
		} else {
			s.Run, s.Timeout = f.Run, f.Timeout
		}
	}
	if s.Hash, err = hashDir(dir); err != nil {
		return Skill{}, err
	}
	s.Trusted = trust != nil && trust.Has(vault, name, s.Hash)
	return s, nil
}

// frontMatter is the key: value lines of a leading "---" block.
func frontMatter(md []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return m
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return m
}

// skipInHash are what running a skill leaves in its folder.
func skipInHash(name string) bool {
	return name == "__pycache__" || name == "node_modules" || strings.HasSuffix(name, ".pyc")
}

const maxSkillBytes = 16 << 20

// hashDir is a digest of every file of a skill (path and content).
func hashDir(dir string) (string, error) {
	h := sha256.New()
	total := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipInHash(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if total += len(b); total > maxSkillBytes {
			return fmt.Errorf("skill %s is over %d MB", dir, maxSkillBytes>>20)
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Command is s's command with its placeholders filled in.
func (s Skill) Command(notebook string) []string {
	r := strings.NewReplacer("{skill}", s.Dir, "{out}", s.Out, "{root}", notebook, "{notebook}", notebook)
	out := make([]string, len(s.Run))
	for i, a := range s.Run {
		if b := r.Replace(a); b != a {
			out[i] = filepath.FromSlash(b) // "{skill}/make.py" is a path: one kind of slash
		} else {
			out[i] = a
		}
	}
	return out
}

// Result is what one run did.
type Result struct {
	Skill    string   `json:"skill"`
	Command  []string `json:"command"`
	Exit     int      `json:"exit"`
	Ms       int64    `json:"ms"`
	Out      string   `json:"out"`
	Files    []string `json:"files"` // what is in the output folder now
	Log      string   `json:"log"`   // the output folder's last.log: all it printed
	Tail     string   `json:"tail"`  // the end of it
	TimedOut bool     `json:"timedOut,omitempty"`
}

// Run runs s in root (the folder its vault is in) with extra arguments;
// env is the app's own variables, added to the series' (JUS_ROOT, …). It
// needs s trusted: the caller asks the user first.
func Run(ctx context.Context, s Skill, root string, env, args []string) (Result, error) {
	notebook := root
	if s.Problem != "" {
		return Result{}, errors.New(s.Problem)
	}
	if s.Run == nil {
		return Result{}, fmt.Errorf("skill %s has nothing to run (no skill.json): it is instructions, SKILL.md", s.Name)
	}
	if !s.Trusted {
		return Result{}, errors.New("not confirmed")
	}
	if err := os.MkdirAll(s.Out, 0o755); err != nil {
		return Result{}, err
	}
	timeout := time.Duration(s.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := append(s.Command(notebook), args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = notebook
	cmd.Env = append(append(os.Environ(),
		"JUS_ROOT="+notebook, "JUS_OUT="+s.Out, "JUS_SKILL="+s.Dir, // the series' names
		"JUS_HARNESS=skill/"+s.Name), env...)
	logPath := filepath.Join(s.Out, "last.log")
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	hideWindow(cmd)
	start := time.Now()
	err := cmd.Run()
	res := Result{Skill: s.Name, Command: argv, Ms: time.Since(start).Milliseconds(), Out: s.Out, Log: logPath}
	os.WriteFile(logPath, buf.Bytes(), 0o644)
	res.Tail = tail(buf.String(), 4000)
	var ee *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut, res.Exit = true, -1
	case errors.As(err, &ee):
		res.Exit = ee.ExitCode()
	case err != nil:
		return res, fmt.Errorf("could not run %s: %w", argv[0], err)
	}
	res.Files = listOut(s.Out)
	return res, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < 200 {
		s = s[i+1:]
	}
	return "…\n" + s
}

// listOut is the output folder's files (relative, "/"), without last.log.
func listOut(dir string) []string {
	out := []string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel = filepath.ToSlash(rel); rel != "last.log" {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// Trust is the app's record of the skills the user said yes to: the
// notebook, the skill and the version (hash) seen.
type Trust struct {
	mu   sync.Mutex
	path string
}

// OpenTrust is the record in the app's folder dir.
func OpenTrust(dir string) *Trust { return &Trust{path: filepath.Join(dir, "skills.json")} }

type trustFile struct {
	Version int               `json:"version"`
	Skills  map[string]string `json:"skills"` // notebook vault|name -> hash
}

func trustKey(vault, name string) string {
	return strings.ToLower(filepath.Clean(vault)) + "|" + name
}

func (t *Trust) read() trustFile {
	f := trustFile{Version: 1, Skills: map[string]string{}}
	if b, err := os.ReadFile(t.path); err == nil {
		json.Unmarshal(b, &f)
		if f.Skills == nil {
			f.Skills = map[string]string{}
		}
	}
	return f
}

// Has reports whether this version of the skill was confirmed.
func (t *Trust) Has(vault, name, hash string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.read().Skills[trustKey(vault, name)] == hash
}

// Add records that the user said yes to this version.
func (t *Trust) Add(vault, name, hash string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.read()
	f.Skills[trustKey(vault, name)] = hash
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	tmp := t.path + ".partial"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}
