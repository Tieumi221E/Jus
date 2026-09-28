package capreg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testRegistry() *Registry {
	r := New("demo", "1.0")
	r.Add(Cap{ID: "progress.set", Summary: "set", Writes: true,
		Params: []Param{
			{Name: "entry", Kind: String, Required: true, Positional: true, Doc: "entry"},
			{Name: "position", Kind: Number, Doc: "seconds"},
			{Name: "watched", Kind: Bool, Doc: "mark watched"},
			{Name: "limit", Kind: Int, Default: int64(10), Doc: "n"},
			{Name: "mode", Kind: String, Enum: []string{"a", "b"}, Doc: "m"},
		},
		Run: func(ctx context.Context, a Args) (any, error) {
			return map[string]any{"entry": a.String("entry"), "position": a.Float("position"), "watched": a.Bool("watched"), "limit": a.Int("limit")}, nil
		}})
	r.Add(Cap{ID: "folder.remove", Summary: "remove", Writes: true, Confirm: true,
		Params: []Param{{Name: "path", Kind: Path, Required: true, Positional: true, Doc: "folder"}},
		Run: func(ctx context.Context, a Args) (any, error) {
			if !a.Confirmed() {
				return nil, NeedConfirm("would remove", map[string]any{"remove": a.String("path")})
			}
			return nil, nil
		}})
	r.Add(Cap{ID: "version", Summary: "version", Run: func(ctx context.Context, a Args) (any, error) { return "1.0", nil }})
	return r
}

func TestResolveAndParse(t *testing.T) {
	r := testRegistry()
	c, rest, ok := r.Resolve([]string{"progress", "set", "e1", "-position", "1:02.5", "-watched", "-json"})
	if !ok || c.ID != "progress.set" {
		t.Fatalf("resolve: %v %v", c, ok)
	}
	inv, err := c.ParseCLI(rest, nil)
	p := inv.Params
	if err != nil || !inv.JSON {
		t.Fatalf("parse: %v %v", err, inv.JSON)
	}
	if p["entry"] != "e1" || p["position"] != 62.5 || p["watched"] != true {
		t.Fatalf("params %v", p)
	}
	// Flags before the positional, = form, negative numbers as values.
	inv, err = c.ParseCLI([]string{"-position=-3", "e2", "-watched=false"}, nil)
	p = inv.Params
	if err != nil || p["entry"] != "e2" || p["position"] != -3.0 || p["watched"] != false {
		t.Fatalf("params %v %v", p, err)
	}
	if _, _, ok := r.Resolve([]string{"version"}); !ok {
		t.Fatal("single-word id")
	}
	if _, _, ok := r.Resolve([]string{"nope"}); ok {
		t.Fatal("unknown resolved")
	}
}

func TestParseErrorsAreUsage(t *testing.T) {
	c, _ := testRegistry().Get("progress.set")
	for _, args := range [][]string{
		{"e", "-nope", "1"},        // unknown flag
		{"e", "extra"},             // too many positionals
		{"e", "-position"},         // flag without value
		{"e", "-position", "x"},    // not a number
		{"e", "-position", "1:75"}, // not a time
		{"e", "-limit", "1.5"},     // not an int
	} {
		if _, err := c.ParseCLI(args, nil); ExitCode(err) != ExitUsage {
			t.Errorf("%v: want usage error, got %v", args, err)
		}
	}
}

func TestCallValidates(t *testing.T) {
	r := testRegistry()
	ctx := context.Background()
	v, err := r.Call(ctx, "progress.set", json.RawMessage(`{"entry":"e","position":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.(map[string]any)["limit"] != int64(10) {
		t.Fatalf("default not applied: %v", v)
	}
	for _, raw := range []string{
		`{}`,                        // missing required
		`{"entry":1}`,               // wrong kind
		`{"entry":"e","bogus":1}`,   // unknown
		`{"entry":"e","mode":"c"}`,  // not in enum
		`{"entry":"e","limit":2.5}`, // not an int
		`[1]`,                       // not an object
	} {
		if _, err := r.Call(ctx, "progress.set", json.RawMessage(raw)); ExitCode(err) != ExitUsage {
			t.Errorf("%s: want usage error, got %v", raw, err)
		}
	}
	if _, err := r.Call(ctx, "nope", nil); ExitCode(err) != ExitUsage {
		t.Errorf("unknown capability: %v", err)
	}
}

func TestConfirm(t *testing.T) {
	r := testRegistry()
	_, err := r.Call(context.Background(), "folder.remove", json.RawMessage(`{"path":"/x"}`))
	e := AsError(err)
	if e.Kind != KindConfirm || e.Plan == nil || ExitCode(err) != ExitUsage {
		t.Fatalf("want a plan and confirm error, got %#v", e)
	}
	if _, err := r.Call(context.Background(), "folder.remove", json.RawMessage(`{"path":"/x","yes":true}`)); err != nil {
		t.Fatal(err)
	}
	// "yes" is only accepted by Confirm capabilities' Decode, never passed
	// through as a real parameter.
	c, _ := r.Get("progress.set")
	a, err := c.Decode(json.RawMessage(`{"entry":"e","yes":true}`))
	if err != nil || a.Has("yes") {
		t.Fatalf("yes leaked into a non-confirm call: %v %v", a, err)
	}
}

func TestPathsMadeAbsolute(t *testing.T) {
	c, _ := testRegistry().Get("folder.remove")
	inv, err := c.ParseCLI([]string{"rel"}, func(s string) (string, error) { return "/abs/" + s, nil })
	if err != nil || inv.Params["path"] != "/abs/rel" {
		t.Fatalf("%v %v", inv.Params, err)
	}
}

func TestHTTP(t *testing.T) {
	r := testRegistry()
	srv := httptest.NewServer(http.StripPrefix("/tok", r))
	defer srv.Close()
	rm := Remote{Base: srv.URL + "/tok/"}
	if !rm.Alive(context.Background()) {
		t.Fatal("not alive")
	}
	v, err := rm.Call(context.Background(), "progress.set", map[string]any{"entry": "e", "position": 2.5})
	if err != nil {
		t.Fatal(err)
	}
	if m := v.(map[string]any); m["entry"] != "e" || m["position"].(json.Number).String() != "2.5" {
		t.Fatalf("%v", v)
	}
	// Errors keep their kind across the wire.
	_, err = rm.Call(context.Background(), "folder.remove", map[string]any{"path": "/x"})
	if e := AsError(err); e.Kind != KindConfirm || e.Plan == nil {
		t.Fatalf("remote confirm: %#v", e)
	}
	_, err = rm.Call(context.Background(), "progress.set", map[string]any{})
	if ExitCode(err) != ExitUsage {
		t.Fatalf("remote usage: %v", err)
	}
	// Only JSON bodies are calls.
	res, err := http.Post(srv.URL+"/tok/api/cap/version", "text/plain", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain call: %s", res.Status)
	}
	// GET api/cap is the help document.
	res, err = http.Get(srv.URL + "/tok/api/cap")
	if err != nil {
		t.Fatal(err)
	}
	var h HelpDoc
	json.NewDecoder(res.Body).Decode(&h)
	res.Body.Close()
	if h.App != "demo" || len(h.Commands) != 3 {
		t.Fatalf("help %+v", h)
	}
}

func TestUsageAndHelpCoverEverything(t *testing.T) {
	r := testRegistry()
	var b bytes.Buffer
	r.Usage(&b, "")
	for _, c := range r.All() {
		if !strings.Contains(b.String(), c.Synopsis()) {
			t.Errorf("usage lacks %s", c.Synopsis())
		}
	}
	if !strings.Contains(b.String(), "progress set <entry> [-position number] [-watched] [-limit int] [-mode a|b] [-harness h -run id]") {
		t.Errorf("synopsis:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "folder remove <path> [-yes]") {
		t.Errorf("confirm synopsis:\n%s", b.String())
	}
}

func TestAddRejectsMalformed(t *testing.T) {
	for name, c := range map[string]Cap{
		"no run":          {ID: "a", Summary: "s"},
		"reserved":        {ID: "a", Summary: "s", Params: []Param{{Name: "json", Kind: Bool}}, Run: func(context.Context, Args) (any, error) { return nil, nil }},
		"no kind":         {ID: "a", Summary: "s", Params: []Param{{Name: "x"}}, Run: func(context.Context, Args) (any, error) { return nil, nil }},
		"bool positional": {ID: "a", Summary: "s", Params: []Param{{Name: "x", Kind: Bool, Positional: true}}, Run: func(context.Context, Args) (any, error) { return nil, nil }},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: accepted", name)
				}
			}()
			New("d", "1").Add(c)
		}()
	}
}

func TestExitCodes(t *testing.T) {
	cases := map[error]int{
		nil:                   ExitOK,
		errors.New("x"):       ExitFail,
		NotFoundf("x"):        ExitFail,
		Usagef("x"):           ExitUsage,
		Conflictf("x"):        ExitConflict,
		NeedConfirm("x", nil): ExitUsage,
	}
	for err, want := range cases {
		if got := ExitCode(err); got != want {
			t.Errorf("%v: %d, want %d", err, got, want)
		}
	}
}

func TestSourceFlagsAndHeaders(t *testing.T) {
	t.Setenv("JUS_HARNESS", "from-env")
	t.Setenv("JUS_RUN", "")
	c, _ := testRegistry().Get("progress.set")
	inv, err := c.ParseCLI([]string{"e", "-run", "s-123", "-model=acme/m1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := inv.Source; s.Via != "cli" || s.Harness != "from-env" || s.Run != "s-123" || s.Model != "acme/m1" {
		t.Fatalf("source %+v", s)
	}
	if _, ok := inv.Params["run"]; ok {
		t.Fatal("a source flag became a parameter")
	}
	// Across the API the source arrives in the call's context.
	var got Source
	r := New("d", "1")
	r.Add(Cap{ID: "who", Summary: "who", Run: func(ctx context.Context, a Args) (any, error) { got = SourceOf(ctx); return nil, nil }})
	srv := httptest.NewServer(r)
	defer srv.Close()
	rm := Remote{Base: srv.URL + "/", Source: inv.Source}
	if _, err := rm.Call(context.Background(), "who", nil); err != nil {
		t.Fatal(err)
	}
	if got != inv.Source {
		t.Fatalf("remote source %+v, want %+v", got, inv.Source)
	}
}

func TestStream(t *testing.T) {
	r := New("d", "1")
	r.Add(Cap{ID: "count", Summary: "count", Params: []Param{{Name: "n", Kind: Int, Required: true, Doc: "n"}},
		Stream: func(ctx context.Context, a Args, emit func(any) error) error {
			for i := int64(0); i < a.Int("n"); i++ {
				if err := emit(map[string]any{"i": i}); err != nil {
					return err
				}
			}
			return nil
		}})
	if _, err := r.Call(context.Background(), "count", json.RawMessage(`{"n":1}`)); ExitCode(err) != ExitUsage {
		t.Fatalf("a stream was called: %v", err)
	}
	srv := httptest.NewServer(r)
	defer srv.Close()
	var got []string
	err := Remote{Base: srv.URL + "/"}.Stream(context.Background(), "count", map[string]any{"n": 3}, func(v any) error {
		got = append(got, v.(map[string]any)["i"].(json.Number).String())
		return nil
	})
	if err != nil || strings.Join(got, ",") != "0,1,2" {
		t.Fatalf("%v %v", got, err)
	}
	if err := (Remote{Base: srv.URL + "/"}).Stream(context.Background(), "count", map[string]any{}, func(any) error { return nil }); ExitCode(err) != ExitUsage {
		t.Fatalf("bad stream parameters: %v", err)
	}
}

func TestStringsAndStdin(t *testing.T) {
	r := New("d", "1")
	var got Args
	r.Add(Cap{ID: "commit", Summary: "commit", Params: []Param{
		{Name: "paths", Kind: Strings, Positional: true, Doc: "files"},
		{Name: "tag", Kind: Strings, Doc: "tags"},
		{Name: "text", Kind: String, Stdin: true, Doc: "text"},
	}, Run: func(ctx context.Context, a Args) (any, error) { got = a; return nil, nil }})
	c, _ := r.Get("commit")
	inv, err := c.ParseCLI([]string{"a.md", "-tag", "x", "b.md", "-tag=y", "c.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inv.Params)
	if _, err := r.Call(context.Background(), "commit", b); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Strings("paths"), ",") != "a.md,b.md,c.md" || strings.Join(got.Strings("tag"), ",") != "x,y" {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(c.Synopsis(), "commit [paths...]") {
		t.Fatalf("synopsis %s", c.Synopsis())
	}
	if _, err := r.Call(context.Background(), "commit", json.RawMessage(`{"paths":"a"}`)); ExitCode(err) != ExitUsage {
		t.Fatalf("a string for a list: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("a positional after a list was accepted")
		}
	}()
	New("d", "1").Add(Cap{ID: "x", Summary: "x", Params: []Param{{Name: "a", Kind: Strings, Positional: true}, {Name: "b", Kind: String, Positional: true}},
		Run: func(context.Context, Args) (any, error) { return nil, nil }})
}

func TestFillStdin(t *testing.T) {
	c := &Cap{ID: "write", Params: []Param{{Name: "text", Kind: String, Stdin: true}, {Name: "file", Kind: Path}}}
	pipe := func(s string) *os.File {
		r, w, _ := os.Pipe()
		w.WriteString(s)
		w.Close()
		return r
	}
	p := map[string]any{}
	c.FillStdin(p, pipe("hello"))
	if p["text"] != "hello" {
		t.Fatalf("piped text: %v", p)
	}
	p = map[string]any{}
	c.FillStdin(p, pipe(""))
	if _, ok := p["text"]; ok {
		t.Fatal("an empty pipe became input")
	}
	p = map[string]any{"file": "x"}
	c.FillStdin(p, pipe("hello"), "file")
	if _, ok := p["text"]; ok {
		t.Fatal("stdin read although -file was given")
	}
	p = map[string]any{"text": "given"}
	c.FillStdin(p, pipe("hello"))
	if p["text"] != "given" {
		t.Fatal("stdin replaced -text")
	}
}
