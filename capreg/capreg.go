// Package capreg is the one description of what the app can do. Every
// capability is registered once, and the command line, `help -json`, the
// agent guide and the window's API (POST api/cap/<id>) are all made from
// the registry, so the window has no ability the command line lacks.
// Every Jus app registers its capabilities here.
package capreg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// Kind is the type of a parameter.
type Kind string

const (
	String Kind = "string"
	Int    Kind = "int"
	Number Kind = "number"
	Bool   Kind = "bool"
	// Path is a string naming a file or folder; the command line makes it
	// absolute before the call, since a running instance has another
	// working directory.
	Path Kind = "path"
	// Ref is an id or a path: the command line makes it absolute when it
	// names an existing file or folder.
	Ref Kind = "ref"
	// Strings is a list of strings: a positional one takes the rest of the
	// positional arguments; a flag may be given more than once.
	Strings Kind = "strings"
)

// Param is one parameter of a capability.
type Param struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	Doc  string `json:"doc"`
	// Required parameters must be given; the others take Default (or are
	// absent from Args when there is none).
	Required bool `json:"required,omitempty"`
	// Positional parameters are given in order on the command line,
	// without a flag name.
	Positional bool `json:"positional,omitempty"`
	Default    any  `json:"default,omitempty"`
	// Enum, if set, lists the accepted values of a string.
	Enum []string `json:"enum,omitempty"`
	// Stdin: on the command line, this parameter may come from standard
	// input when it is not given (an empty input is no input).
	Stdin bool `json:"stdin,omitempty"`
}

// Cap is one capability.
type Cap struct {
	// ID is "group.verb" (the command line spells it "group verb") or a
	// single word.
	ID      string  `json:"id"`
	Summary string  `json:"summary"`
	Params  []Param `json:"params,omitempty"`
	// Writes: it changes files (the user's or the app's records).
	Writes bool `json:"writes"`
	// Confirm: it is hard to undo; the window asks first, the command line
	// wants -yes.
	Confirm bool `json:"confirm,omitempty"`
	// Window: it needs the app's window (decoding, playback), so the
	// command line hands it to a running instance.
	Window bool `json:"window,omitempty"`

	Run func(ctx context.Context, a Args) (any, error) `json:"-"`
	// Stream, instead of Run, gives a stream of results (one JSON line
	// each) until ctx ends: `watch`. The command line prints them as they
	// come; the window's API serves them on GET api/cap/<id>.
	Stream func(ctx context.Context, a Args, emit func(any) error) error `json:"-"`
	// Streams is set for Stream capabilities (for help -json).
	Streams bool `json:"stream,omitempty"`
	// Text, if set, prints a result for people (the command line without
	// -json); otherwise the result is printed as indented JSON. It gets the
	// result as decoded from JSON (map[string]any, []any, json.Number …),
	// the same whether the call ran here or in a running instance.
	Text func(w io.Writer, v any) `json:"-"`
}

// Words is the command-line spelling of the ID.
func (c *Cap) Words() []string { return strings.Split(c.ID, ".") }

// Registry holds the capabilities of an app.
type Registry struct {
	App     string
	Version string
	caps    []*Cap
	byID    map[string]*Cap
}

func New(app, version string) *Registry {
	return &Registry{App: app, Version: version, byID: map[string]*Cap{}}
}

// Add registers c. It panics on a malformed or duplicate capability: the
// table is part of the program, not input.
func (r *Registry) Add(c Cap) {
	if c.ID == "" || c.Summary == "" || (c.Run == nil) == (c.Stream == nil) {
		panic("capreg: capability without id, summary, or exactly one of run and stream: " + c.ID)
	}
	c.Streams = c.Stream != nil
	if _, dup := r.byID[c.ID]; dup {
		panic("capreg: duplicate capability " + c.ID)
	}
	seen := map[string]bool{"json": true, "yes": true, "data": true}
	for k := range sourceFlags {
		seen[k] = true
	}
	optional, restTaken := false, false
	for _, p := range c.Params {
		if seen[p.Name] {
			panic(fmt.Sprintf("capreg: %s: parameter %q repeated or reserved", c.ID, p.Name))
		}
		seen[p.Name] = true
		switch p.Kind {
		case String, Int, Number, Bool, Path, Ref, Strings:
		default:
			panic(fmt.Sprintf("capreg: %s: parameter %q has no kind", c.ID, p.Name))
		}
		if p.Positional {
			if restTaken {
				panic(fmt.Sprintf("capreg: %s: positional %q after a positional list", c.ID, p.Name))
			}
			restTaken = p.Kind == Strings
			if p.Kind == Bool {
				panic(fmt.Sprintf("capreg: %s: positional parameter %q cannot be a bool", c.ID, p.Name))
			}
			if optional && p.Required {
				panic(fmt.Sprintf("capreg: %s: required positional %q after an optional one", c.ID, p.Name))
			}
			if !p.Required {
				optional = true
			}
		}
	}
	cp := c
	r.caps = append(r.caps, &cp)
	r.byID[c.ID] = &cp
}

// Get returns the capability with this ID.
func (r *Registry) Get(id string) (*Cap, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// All returns the capabilities sorted by ID.
func (r *Registry) All() []*Cap {
	out := append([]*Cap(nil), r.caps...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Call validates raw (a JSON object of parameters) against the
// capability and runs it.
func (r *Registry) Call(ctx context.Context, id string, raw json.RawMessage) (any, error) {
	c, ok := r.byID[id]
	if !ok {
		return nil, Usagef("unknown capability %q", id)
	}
	if c.Stream != nil {
		return nil, Usagef("%s gives a stream of results; it is not a call", c.Command())
	}
	a, err := c.Decode(raw)
	if err != nil {
		return nil, err
	}
	return c.Run(ctx, a)
}

// Stream runs a Stream capability, calling emit with each result.
func (r *Registry) Stream(ctx context.Context, id string, raw json.RawMessage, emit func(any) error) error {
	c, ok := r.byID[id]
	if !ok {
		return Usagef("unknown capability %q", id)
	}
	if c.Stream == nil {
		return Usagef("%s is a call, not a stream", c.Command())
	}
	a, err := c.Decode(raw)
	if err != nil {
		return err
	}
	return c.Stream(ctx, a, emit)
}

// Decode checks raw against the parameters: unknown names, wrong kinds and
// missing required ones are usage errors; defaults fill the rest.
func (c *Cap) Decode(raw json.RawMessage) (Args, error) {
	in := map[string]json.RawMessage{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, Usagef("%s: parameters must be a JSON object: %v", c.Command(), err)
		}
	}
	known := map[string]Param{}
	for _, p := range c.Params {
		known[p.Name] = p
	}
	for k := range in {
		if _, ok := known[k]; !ok && k != "yes" {
			return nil, Usagef("%s: unknown parameter %q", c.Command(), k)
		}
	}
	a := Args{}
	for _, p := range c.Params {
		v, ok := in[p.Name]
		if !ok || string(v) == "null" {
			if p.Required {
				return nil, Usagef("%s: missing %s", c.Command(), p.Name)
			}
			if p.Default != nil {
				a[p.Name] = p.Default
			}
			continue
		}
		x, err := decodeValue(p, v)
		if err != nil {
			return nil, Usagef("%s: %s: %v", c.Command(), p.Name, err)
		}
		a[p.Name] = x
	}
	if c.Confirm {
		var yes bool
		if v, ok := in["yes"]; ok && json.Unmarshal(v, &yes) == nil && yes {
			a["yes"] = true
		}
	}
	return a, nil
}

func decodeValue(p Param, v json.RawMessage) (any, error) {
	switch p.Kind {
	case String, Path, Ref:
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, errors.New("want a string")
		}
		if len(p.Enum) > 0 && !contains(p.Enum, s) {
			return nil, fmt.Errorf("want one of %s", strings.Join(p.Enum, ", "))
		}
		return s, nil
	case Int:
		var f float64
		if err := json.Unmarshal(v, &f); err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
			return nil, errors.New("want an integer")
		}
		return int64(f), nil
	case Number:
		var f float64
		if err := json.Unmarshal(v, &f); err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, errors.New("want a number")
		}
		return f, nil
	case Bool:
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			return nil, errors.New("want true or false")
		}
		return b, nil
	case Strings:
		var ss []string
		if err := json.Unmarshal(v, &ss); err != nil {
			return nil, errors.New("want a list of strings")
		}
		return ss, nil
	}
	return nil, errors.New("unknown kind")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Args are a call's parameters after Decode.
type Args map[string]any

func (a Args) Has(k string) bool { _, ok := a[k]; return ok }

func (a Args) String(k string) string {
	s, _ := a[k].(string)
	return s
}

func (a Args) Int(k string) int64 {
	switch v := a[k].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func (a Args) Float(k string) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

func (a Args) Strings(k string) []string {
	switch v := a[k].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (a Args) Bool(k string) bool {
	b, _ := a[k].(bool)
	return b
}

// Confirmed reports whether a Confirm capability was called with yes.
func (a Args) Confirmed() bool { return a.Bool("yes") }
