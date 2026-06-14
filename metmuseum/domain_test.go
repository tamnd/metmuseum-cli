package metmuseum

import (
	"testing"
)

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "metmuseum" {
		t.Errorf("Scheme = %q, want metmuseum", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "metmuseum" {
		t.Errorf("Identity.Binary = %q, want metmuseum", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	typ, id, err := Domain{}.Classify("436532")
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}
	if typ != "object" {
		t.Errorf("type = %q, want object", typ)
	}
	if id != "436532" {
		t.Errorf("id = %q, want 436532", id)
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("object", "436532")
	if err != nil {
		t.Fatalf("Locate error: %v", err)
	}
	if got == "" {
		t.Error("Locate returned empty URL")
	}
}
