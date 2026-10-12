package taskhook

import (
	"strings"
	"testing"
)

func TestContextIdentitySeparatesMachineUIDAndCanonicalHome(t *testing.T) {
	base := TargetIdentity{Version: 1, UID: 1000, Machine: "linux-machine-id:0123456789abcdef0123456789abcdef",
		AccountHome: "/workspace/account", CodexHome: "/workspace/account/.codex"}
	id, err := ContextID(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 69 || !strings.HasPrefix(id, "ctx1-") {
		t.Fatal("invalid domain-separated identity")
	}
	for _, mutate := range []func(*TargetIdentity){
		func(p *TargetIdentity) { p.UID++ },
		func(p *TargetIdentity) { p.Machine = "linux-machine-id:1123456789abcdef0123456789abcdef" },
		func(p *TargetIdentity) { p.CodexHome = "/workspace/account/custom" },
	} {
		other := base
		mutate(&other)
		otherID, e := ContextID(other)
		if e != nil || otherID == id {
			t.Fatal("target identities collapsed")
		}
	}
	bad := base
	bad.CodexHome = "relative"
	if _, err = ContextID(bad); err == nil {
		t.Fatal("relative home accepted")
	}
}
