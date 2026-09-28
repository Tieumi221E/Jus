package capreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Resolve finds the capability the first words of args name ("library
// list …" is library.list) and returns the rest of the arguments.
func (r *Registry) Resolve(args []string) (*Cap, []string, bool) {
	if len(args) >= 2 {
		if c, ok := r.byID[args[0]+"."+args[1]]; ok {
			return c, args[2:], true
		}
	}
	if len(args) >= 1 {
		if c, ok := r.byID[args[0]]; ok {
			return c, args[1:], true
		}
	}
	return nil, nil, false
}

var numberLike = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)$`)

// Invocation is a command line, parsed.
type Invocation struct {
	Params map[string]any // the call's parameters, as the JSON object Call takes
	JSON   bool           // -json: machine output
	Source Source         // who is calling: -author -harness -model -run, else JUS_* variables
}

// ParseCLI turns command-line arguments into c's parameters. Flags are
// -name value or -name=value (a bool may be just -name); positional
// parameters come in order, before or after the flags; "--" ends the
// flags. -json, -yes and the source flags are the same for every command.
// abs makes Path parameters absolute.
func (c *Cap) ParseCLI(args []string, abs func(string) (string, error)) (inv Invocation, err error) {
	inv.Source = SourceFromEnv("cli")
	params := map[string]any{}
	inv.Params = params
	var asJSON bool
	defer func() { inv.JSON = asJSON }()
	byName := map[string]Param{}
	var positional []Param
	for _, p := range c.Params {
		byName[p.Name] = p
		if p.Positional {
			positional = append(positional, p)
		}
	}
	var pos []string
	flags := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		if flags && a == "--" {
			flags = false
			continue
		}
		if !flags || !strings.HasPrefix(a, "-") || a == "-" || numberLike.MatchString(a) {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch name {
		case "json":
			asJSON = true
			continue
		case "yes":
			params["yes"] = true
			continue
		}
		if _, ok := sourceFlags[name]; ok {
			if !hasVal {
				if i+1 >= len(args) {
					return inv, Usagef("%s: -%s needs a value", c.Command(), name)
				}
				i++
				val = args[i]
			}
			inv.Source.set(name, val)
			continue
		}
		p, ok := byName[name]
		if !ok {
			return inv, Usagef("%s: unknown flag -%s", c.Command(), name)
		}
		if p.Kind == Bool && !hasVal {
			params[name] = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return inv, Usagef("%s: -%s needs a value", c.Command(), name)
			}
			i++
			val = args[i]
		}
		if p.Kind == Strings {
			prev, _ := params[name].([]string)
			params[name] = append(prev, val)
			continue
		}
		v, err := cliValue(p, val, abs)
		if err != nil {
			return inv, Usagef("%s: -%s: %v", c.Command(), name, err)
		}
		params[name] = v
	}
	if n := len(positional); n > 0 && positional[n-1].Kind == Strings && len(pos) >= n {
		// The last positional is a list: it takes the rest.
		params[positional[n-1].Name] = append([]string(nil), pos[n-1:]...)
		pos = pos[:n-1]
		positional = positional[:n-1]
	}
	if len(pos) > len(positional) {
		return inv, Usagef("%s: too many arguments (%s)", c.Command(), strings.Join(pos[len(positional):], " "))
	}
	for i, s := range pos {
		p := positional[i]
		if _, dup := params[p.Name]; dup {
			return inv, Usagef("%s: %s given twice", c.Command(), p.Name)
		}
		v, err := cliValue(p, s, abs)
		if err != nil {
			return inv, Usagef("%s: %s: %v", c.Command(), p.Name, err)
		}
		params[p.Name] = v
	}
	return inv, nil
}

func cliValue(p Param, s string, abs func(string) (string, error)) (any, error) {
	switch p.Kind {
	case Int:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("want an integer, got %q", s)
		}
		return n, nil
	case Number:
		f, err := parseNumber(s)
		if err != nil {
			return nil, err
		}
		return f, nil
	case Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("want true or false, got %q", s)
		}
		return b, nil
	case Path:
		if abs != nil && s != "" {
			return abs(s)
		}
	case Ref:
		if _, err := os.Stat(s); err == nil && abs != nil {
			return abs(s)
		}
	}
	return s, nil
}

// parseNumber reads a number, also as a time: "83.5", "1:23.5", "0:01:23".
func parseNumber(s string) (float64, error) {
	if !strings.Contains(s, ":") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("want a number, got %q", s)
		}
		return f, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("want seconds or [h:]m:s, got %q", s)
	}
	var t float64
	for i, x := range parts {
		f, err := strconv.ParseFloat(x, 64)
		if err != nil || f < 0 || (i > 0 && f >= 60) {
			return 0, fmt.Errorf("want seconds or [h:]m:s, got %q", s)
		}
		t = t*60 + f
	}
	return t, nil
}

// Command is the command-line spelling: "library list".
func (c *Cap) Command() string { return strings.ReplaceAll(c.ID, ".", " ") }

// Synopsis is the command with its parameters: "progress set <entry> [-position n]".
func (c *Cap) Synopsis() string {
	var b strings.Builder
	b.WriteString(c.Command())
	for _, p := range c.Params {
		if p.Positional {
			name := p.Name
			if p.Kind == Strings {
				name += "..."
			}
			if p.Required {
				fmt.Fprintf(&b, " <%s>", name)
			} else {
				fmt.Fprintf(&b, " [%s]", name)
			}
		}
	}
	for _, p := range c.Params {
		if p.Positional {
			continue
		}
		v := " " + string(p.Kind)
		if p.Kind == Bool {
			v = ""
		} else if len(p.Enum) > 0 {
			v = " " + strings.Join(p.Enum, "|")
		}
		if p.Required {
			fmt.Fprintf(&b, " -%s%s", p.Name, v)
		} else {
			fmt.Fprintf(&b, " [-%s%s]", p.Name, v)
		}
	}
	if c.Confirm {
		b.WriteString(" [-yes]")
	}
	if c.Writes {
		b.WriteString(" [-harness h -run id]")
	}
	return b.String()
}

// Usage writes the command list for people, grouped by the first word.
func (r *Registry) Usage(w io.Writer, extra string) {
	fmt.Fprintf(w, "%s %s\n\n", r.App, r.Version)
	groups := map[string][]*Cap{}
	var names []string
	for _, c := range r.All() {
		g := c.Words()[0]
		if len(c.Words()) == 1 {
			g = ""
		}
		if _, ok := groups[g]; !ok {
			names = append(names, g)
		}
		groups[g] = append(groups[g], c)
	}
	sort.Strings(names)
	for _, g := range names {
		for _, c := range groups[g] {
			fmt.Fprintf(w, "  %s\n      %s\n", c.Synopsis(), c.Summary)
		}
		fmt.Fprintln(w)
	}
	if extra != "" {
		fmt.Fprint(w, extra)
	}
	fmt.Fprintf(w, "Every command takes -json (machine output); `%s help -json` describes them all.\n", r.App)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 failed, 2 usage (or -yes needed), 3 conflict (the data changed).")
}

// HelpDoc is `help -json`.
type HelpDoc struct {
	App       string            `json:"app"`
	Version   string            `json:"version"`
	ExitCodes map[string]string `json:"exit"`
	Notes     []string          `json:"notes"`
	Commands  []HelpCommand     `json:"commands"`
}

type HelpCommand struct {
	*Cap
	Name     string `json:"name"`    // the command-line spelling, as Command
	Command  string `json:"command"` // (the same; kept for the first readers)
	Synopsis string `json:"synopsis"`
}

func (r *Registry) Help(notes ...string) HelpDoc {
	d := HelpDoc{App: r.App, Version: r.Version,
		ExitCodes: map[string]string{"0": "ok", "1": "failed", "2": "usage error, or a confirm command without -yes (its plan is in the -json output)", "3": "conflict: the data changed since the version the call was based on"},
		Notes: append([]string{
			"Positional parameters come in order; flags are -name value or -name=value; a bool flag may be just -name.",
			"number parameters that are times also take [h:]m:s.",
			"-json prints the result as JSON on stdout (a stream command: one JSON object per line); a failure prints {\"error\", \"kind\", \"code\"} on stderr, code being the exit code.",
			"Who is calling: -author human|agent, -harness <name>, -model <provider/model>, -run <session id> (or JUS_AUTHOR, JUS_HARNESS, JUS_MODEL, JUS_RUN); recorded with the changes and in events, never guessed.",
			"When the app's window is open, commands run in it, and it shows their effect at once; window commands need it (it is started when missing).",
		}, notes...)}
	for _, c := range r.All() {
		d.Commands = append(d.Commands, HelpCommand{Cap: c, Name: c.Command(), Command: c.Command(), Synopsis: c.Synopsis()})
	}
	return d
}

// PrintResult writes v for the command line.
func (c *Cap) PrintResult(w io.Writer, v any, asJSON bool) {
	if asJSON || c.Text == nil {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if !asJSON {
			enc.SetIndent("", "  ")
		}
		if v == nil {
			v = map[string]any{"ok": true}
		}
		enc.Encode(v)
		return
	}
	c.Text(w, Generic(v))
}

// Generic is v as JSON would give it back: what Text sees.
func Generic(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var g any
	if dec.Decode(&g) != nil {
		return v
	}
	return g
}

// As is what Text sees (Generic) as a T again: a Text that wants the
// result's own type decodes it with As.
func As[T any](v any) T {
	var t T
	if b, err := json.Marshal(v); err == nil {
		json.Unmarshal(b, &t)
	}
	return t
}

// FillStdin gives c's Stdin parameter what is piped into in when the
// command line did not give it, nor any of the alternatives named (a -file
// parameter, say). An empty pipe is no input: agents' shells often run
// commands with a closed or empty standard input, and that must never
// become "write an empty note". in may be nil (no input).
func (c *Cap) FillStdin(params map[string]any, in *os.File, alternatives ...string) error {
	var p *Param
	for i := range c.Params {
		if c.Params[i].Stdin {
			p = &c.Params[i]
		}
	}
	if p == nil || in == nil {
		return nil
	}
	if _, given := params[p.Name]; given {
		return nil
	}
	for _, a := range alternatives {
		if _, given := params[a]; given {
			return nil
		}
	}
	if st, err := in.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return nil // a terminal, or nothing: no input
	}
	b, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if len(b) > 0 {
		params[p.Name] = string(b)
	}
	return nil
}

// ErrorOut is how a failure is reported with -json, on stderr: the
// message, its kind, the exit code, and for a confirm error the plan.
func ErrorOut(err error) map[string]any {
	e := AsError(err)
	m := map[string]any{"error": e.Msg, "kind": e.Kind, "code": ExitCode(err)}
	if e.Plan != nil {
		m["plan"] = e.Plan
	}
	return m
}
