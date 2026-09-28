package capreg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ServeHTTP is the window's side of the registry, mounted at api/cap/:
//
//	GET  api/cap        help (the same document as `help -json`)
//	POST api/cap/<id>   call; body: the parameters as a JSON object
//
// A call answers 200 with the result as JSON, or an error status (see
// HTTPStatus) with {"error", "kind"[, "plan"]}. Calls only take
// application/json, so a cross-site form cannot make one.
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, "/"), "api/cap")
	id = strings.TrimPrefix(id, "/")
	w.Header().Set("Content-Type", "application/json")
	if id == "" && req.Method == http.MethodGet {
		json.NewEncoder(w).Encode(r.Help())
		return
	}
	ctx := WithSource(req.Context(), sourceFromHeader(req.Header))
	if req.Method == http.MethodGet {
		r.serveStream(w, req.WithContext(ctx), id)
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed","kind":"usage"}`, http.StatusMethodNotAllowed)
		return
	}
	if mt, _, _ := mime.ParseMediaType(req.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, `{"error":"calls take application/json","kind":"usage"}`, http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		writeError(w, Usagef("reading the call: %v", err))
		return
	}
	v, err := r.Call(ctx, id, body)
	if err != nil {
		writeError(w, err)
		return
	}
	if v == nil {
		v = map[string]any{"ok": true}
	}
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(HTTPStatus(err))
	json.NewEncoder(w).Encode(AsError(err))
}

// serveStream is GET api/cap/<id>?p=<parameters as JSON>: the results as
// JSON lines, flushed as they come, until the caller goes away.
func (r *Registry) serveStream(w http.ResponseWriter, req *http.Request, id string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	rc := http.NewResponseController(w)
	started := false
	enc := json.NewEncoder(w)
	err := r.Stream(req.Context(), id, json.RawMessage(req.URL.Query().Get("p")), func(v any) error {
		started = true
		if err := enc.Encode(v); err != nil {
			return err
		}
		return rc.Flush()
	})
	if err != nil && !started && req.Context().Err() == nil {
		writeError(w, err)
	}
}

// Remote calls capabilities of a running instance.
type Remote struct {
	// Base is the instance's page URL, ending in "/" (it contains the
	// instance's random token).
	Base   string
	Client *http.Client
	// Source is who is calling, passed on in headers.
	Source Source
}

// Call posts params to the instance and decodes its answer into a
// generic value (the printed result is the same either way).
func (rm Remote) Call(ctx context.Context, id string, params map[string]any) (any, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rm.Base+"api/cap/"+id, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	rm.Source.header(req.Header)
	c := rm.Client
	if c == nil {
		c = &http.Client{}
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(res.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		var e Error
		if json.Unmarshal(rb, &e) == nil && e.Msg != "" {
			if e.Kind == "" {
				e.Kind = FromStatus(res.StatusCode)
			}
			return nil, &e
		}
		return nil, &Error{Kind: FromStatus(res.StatusCode), Msg: fmt.Sprintf("%s: %s", res.Status, strings.TrimSpace(string(rb)))}
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(rb))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Stream reads a Stream capability's results from the instance, calling
// emit with each (decoded like Call's results) until ctx ends or it stops.
func (rm Remote) Stream(ctx context.Context, id string, params map[string]any, emit func(any) error) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rm.Base+"api/cap/"+id+"?p="+url.QueryEscape(string(b)), nil)
	if err != nil {
		return err
	}
	rm.Source.header(req.Header)
	c := &http.Client{} // no timeout: a stream lasts
	if rm.Client != nil {
		c.Transport = rm.Client.Transport
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		var e Error
		if json.Unmarshal(rb, &e) == nil && e.Msg != "" {
			return &e
		}
		return &Error{Kind: FromStatus(res.StatusCode), Msg: res.Status}
	}
	dec := json.NewDecoder(res.Body)
	dec.UseNumber()
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := emit(v); err != nil {
			return err
		}
	}
}

// Alive reports whether the instance answers.
func (rm Remote) Alive(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rm.Base+"api/cap", nil)
	if err != nil {
		return false
	}
	c := rm.Client
	if c == nil {
		c = &http.Client{}
	}
	res, err := c.Do(req)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}
