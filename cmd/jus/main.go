// Command jus is the series' own small program: which Jus
// apps are on this computer and what each can do (apps), handing a jus://
// link to its app (open), removing an app and everything it wrote except
// your content (uninstall), and the conformance test (conform).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Tieumi221E/Jus/apps"
	"github.com/Tieumi221E/Jus/capreg"
	"github.com/Tieumi221E/Jus/conform"
)

var version = "0.1.0"

func registry() *capreg.Registry {
	r := capreg.New("jus", version)
	r.Add(capreg.Cap{ID: "apps", Summary: "the Jus apps on this computer (beside jus, in folders beside its own, on PATH): each one's manifest — links, formats, footprint",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			found, others := apps.All(ctx)
			if found == nil {
				found = []apps.Manifest{}
			}
			return map[string]any{"apps": found, "unanswered": others}, nil
		},
		Text: func(w io.Writer, v any) {
			m := capreg.As[struct {
				Apps       []apps.Manifest `json:"apps"`
				Unanswered []apps.Found    `json:"unanswered"`
			}](v)
			found := m.Apps
			if len(found) == 0 {
				fmt.Fprintln(w, "no Jus apps found")
			}
			for _, a := range found {
				fmt.Fprintf(w, "%-10s %-12s %s\n", a.ID, a.Version, a.Exe)
			}
			for _, o := range m.Unanswered {
				fmt.Fprintf(w, "(%s: %s)\n", o.Exe, o.Problem)
			}
		}})

	r.Add(capreg.Cap{ID: "open", Summary: "hand a jus:// link to its app (jus://note/… to jusnote, jus://play/… to jusplay)",
		Params: []capreg.Param{{Name: "link", Kind: capreg.String, Required: true, Positional: true}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			link := a.String("link")
			rest, ok := strings.CutPrefix(link, "jus://")
			host, _, _ := strings.Cut(rest, "/")
			if !ok || host == "" {
				return nil, capreg.Usagef("not a jus:// link: %s", link)
			}
			id := "jus" + host
			exe := apps.Find(id)
			if exe == "" {
				return nil, capreg.NotFoundf("%s is not installed (the link needs it)", id)
			}
			args := []string{link}
			if id == "jusnote" {
				args = []string{"open", link} // jusnote takes links through open
			}
			cmd := exec.Command(exe, args...)
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			cmd.Process.Release()
			return map[string]any{"app": id, "exe": exe}, nil
		}})

	r.Add(capreg.Cap{ID: "uninstall", Summary: "remove a Jus app: its program, its data folder, its caches — never your content (notes, record types, watch history), which it lists; without -yes, says what it would remove",
		Writes: true, Confirm: true,
		Params: []capreg.Param{{Name: "app", Kind: capreg.String, Required: true, Positional: true, Doc: "the app (jus apps), or the path of its program"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			exe := a.String("app")
			if !strings.ContainsAny(exe, `/\`) {
				exe = apps.Find(exe)
			}
			if exe == "" {
				return nil, capreg.NotFoundf("no app %s here (jus apps)", a.String("app"))
			}
			man, err := apps.Ask(ctx, exe)
			if err != nil {
				return nil, err
			}
			plan := apps.PlanUninstall(man)
			if !a.Confirmed() {
				return nil, capreg.NeedConfirm("uninstall "+man.ID+" removes these places (your content stays); add -yes", plan)
			}
			return apps.Uninstall(plan), nil
		}})

	r.Add(capreg.Cap{ID: "conform", Summary: "the Jus conformance test of an app's program: capability list, command line, exit codes, manifest, footprint",
		Params: []capreg.Param{{Name: "exe", Kind: capreg.Path, Required: true, Positional: true, Doc: "the app's program"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			dir, err := os.MkdirTemp("", "jus-conform-")
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(dir)
			rep := conform.Run(ctx, a.String("exe"), dir, nil)
			if !rep.OK() {
				return rep, errFailed{rep}
			}
			return rep, nil
		},
		Text: printReport})
	return r
}

type errFailed struct{ rep conform.Report }

func (e errFailed) Error() string { return "conformance failed" }

func printReport(w io.Writer, v any) {
	rep := capreg.As[conform.Report](v)
	for _, c := range rep.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "%s %s", mark, c.Name)
		if !c.OK && c.Detail != "" {
			fmt.Fprintf(w, " — %s", c.Detail)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s: %d checks in %d ms\n", rep.App, len(rep.Checks), rep.Ms)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	r := registry()
	if len(args) == 0 {
		r.Usage(stdout, "")
		return 0
	}
	switch args[0] {
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "jus", version)
		return 0
	case "help", "-h", "-help", "--help":
		if len(args) > 1 && (args[1] == "-json" || args[1] == "--json") {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			enc.Encode(r.Help("jus finds the apps where their release zips put them: beside it, in folders beside its own, on PATH (or JUS_<APP>_EXE)."))
			return 0
		}
		r.Usage(stdout, "")
		return 0
	}
	c, rest, ok := r.Resolve(args)
	if !ok {
		fmt.Fprintf(stderr, "jus: unknown command %q (jus help)\n", args[0])
		return capreg.ExitUsage
	}
	inv, err := c.ParseCLI(rest, filepath.Abs)
	if err == nil {
		err = c.FillStdin(inv.Params, stdin)
	}
	var v any
	if err == nil {
		b, _ := json.Marshal(inv.Params)
		v, err = r.Call(capreg.WithSource(context.Background(), inv.Source), c.ID, b)
	}
	var failed errFailed
	if errors.As(err, &failed) {
		c.PrintResult(stdout, failed.rep, inv.JSON)
		return capreg.ExitFail
	}
	if err != nil {
		if inv.JSON {
			json.NewEncoder(stderr).Encode(capreg.ErrorOut(err))
		} else {
			e := capreg.AsError(err)
			fmt.Fprintln(stderr, "jus:", e.Msg)
			if e.Plan != nil {
				b, _ := json.MarshalIndent(e.Plan, "", "  ")
				fmt.Fprintf(stderr, "it would do:\n%s\n", b)
			}
		}
		return capreg.ExitCode(err)
	}
	c.PrintResult(stdout, v, inv.JSON)
	return 0
}
