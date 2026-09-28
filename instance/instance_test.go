package instance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Tieumi221E/Jus/capreg"
)

func TestAnnounceAndFind(t *testing.T) {
	dir := t.TempDir()
	if _, _, ok := Find(dir); ok {
		t.Fatal("found a window before one announced itself")
	}
	r := capreg.New("demo", "1")
	r.Add(capreg.Cap{ID: "ping", Summary: "ping", Run: func(context.Context, capreg.Args) (any, error) { return "pong", nil }})
	srv := httptest.NewServer(http.StripPrefix("/tok", r))
	defer srv.Close()
	remove := Announce(dir, "demo", "1", srv.URL+"/tok/")
	if st, err := os.Stat(File(dir)); err != nil || st.Size() == 0 {
		t.Fatal("no announcement")
	}
	rm, in, ok := Find(dir)
	if !ok || in.App != "demo" || in.PID != os.Getpid() {
		t.Fatalf("find: %v %+v", ok, in)
	}
	if v, err := rm.Call(context.Background(), "ping", nil); err != nil || v != "pong" {
		t.Fatalf("call: %v %v", v, err)
	}
	remove()
	if _, err := os.Stat(File(dir)); !os.IsNotExist(err) {
		t.Fatal("announcement left behind")
	}
}

// A stale announcement (the window crashed) is not a window.
func TestStaleAnnouncement(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/tok/"
	srv.Close()
	Announce(dir, "demo", "1", url)
	if _, _, ok := Find(dir); ok {
		t.Fatal("a dead window was found")
	}
}
