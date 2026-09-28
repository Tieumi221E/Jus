package capreg

import (
	"context"
	"net/http"
	"os"
)

// Source is who made a call: the window itself, a person at the command
// line, or an agent — which harness, which of its sessions, which model —
// as far as it said (what is not said is not guessed).
type Source struct {
	// Via is how the call came: "window", "cli" or "api".
	Via     string `json:"via"`
	Author  string `json:"author,omitempty"`  // human | agent
	Harness string `json:"harness,omitempty"` // e.g. codex, claude-code
	Model   string `json:"model,omitempty"`   // provider/model
	Run     string `json:"run,omitempty"`     // the harness's session id
}

// The command-line flags and environment variables that say who is calling.
var sourceFlags = map[string]string{"author": "JUS_AUTHOR", "harness": "JUS_HARNESS", "model": "JUS_MODEL", "run": "JUS_RUN"}

// SourceFromEnv is the caller as the environment says (JUS_AUTHOR,
// JUS_HARNESS, JUS_MODEL, JUS_RUN).
func SourceFromEnv(via string) Source {
	return Source{Via: via, Author: os.Getenv("JUS_AUTHOR"), Harness: os.Getenv("JUS_HARNESS"),
		Model: os.Getenv("JUS_MODEL"), Run: os.Getenv("JUS_RUN")}
}

func (s *Source) set(k, v string) {
	switch k {
	case "author":
		s.Author = v
	case "harness":
		s.Harness = v
	case "model":
		s.Model = v
	case "run":
		s.Run = v
	}
}

type sourceKey struct{}

// WithSource gives ctx the caller.
func WithSource(ctx context.Context, s Source) context.Context {
	return context.WithValue(ctx, sourceKey{}, s)
}

// SourceOf is the caller a call's context carries ({Via: "api"} when none).
func SourceOf(ctx context.Context) Source {
	if s, ok := ctx.Value(sourceKey{}).(Source); ok {
		return s
	}
	return Source{Via: "api"}
}

// Across the local API the caller goes in these headers.
var sourceHeaders = map[string]string{"Via": "X-Jus-Via", "Author": "X-Jus-Author", "Harness": "X-Jus-Harness", "Model": "X-Jus-Model", "Run": "X-Jus-Run"}

func (s Source) header(h http.Header) {
	for k, v := range map[string]string{"Via": s.Via, "Author": s.Author, "Harness": s.Harness, "Model": s.Model, "Run": s.Run} {
		if v != "" {
			h.Set(sourceHeaders[k], v)
		}
	}
}

func sourceFromHeader(h http.Header) Source {
	s := Source{Via: h.Get(sourceHeaders["Via"]), Author: h.Get(sourceHeaders["Author"]), Harness: h.Get(sourceHeaders["Harness"]),
		Model: h.Get(sourceHeaders["Model"]), Run: h.Get(sourceHeaders["Run"])}
	if s.Via == "" {
		s.Via = "api"
	}
	return s
}
