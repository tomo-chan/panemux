package board

import "testing"

func TestStaticPaneResolver_HostForPane(t *testing.T) {
	r := NewStaticPaneResolver([]PaneRef{
		{ID: "pane-a", HostID: "local"},
		{ID: "pane-b", HostID: "build-host"},
	})

	host, ok := r.HostForPane("pane-a")
	if !ok || host != "local" {
		t.Fatalf("HostForPane(pane-a) = (%q, %v)", host, ok)
	}
	host, ok = r.HostForPane("pane-b")
	if !ok || host != "build-host" {
		t.Fatalf("HostForPane(pane-b) = (%q, %v)", host, ok)
	}
	if _, ok := r.HostForPane("pane-unknown"); ok {
		t.Fatal("expected unknown pane to resolve to ok=false")
	}
}

func TestStaticPaneResolver_KnownPane(t *testing.T) {
	r := NewStaticPaneResolver([]PaneRef{
		{ID: "pane-a", HostID: "local"},
		{ID: "pane-b", HostID: "build-host"},
	})

	if !r.KnownPane("local", "pane-a") {
		t.Fatal("expected pane-a to be known on local")
	}
	if r.KnownPane("build-host", "pane-a") {
		t.Fatal("pane-a is not on build-host")
	}
	if r.KnownPane("local", "pane-unknown") {
		t.Fatal("unknown pane must not be known")
	}
	if r.KnownPane("nonexistent-host", "pane-a") {
		t.Fatal("unknown host must not know any pane")
	}
}

func TestStaticPaneResolver_Empty(t *testing.T) {
	r := NewStaticPaneResolver(nil)
	if _, ok := r.HostForPane("anything"); ok {
		t.Fatal("expected empty resolver to know nothing")
	}
	if r.KnownPane("local", "anything") {
		t.Fatal("expected empty resolver to know nothing")
	}
}
