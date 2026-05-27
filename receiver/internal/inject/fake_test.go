package inject

import "testing"

func TestFakeInjectorRecordsCalls(t *testing.T) {
	f := &Fake{}
	out, err := f.Inject("hello")
	if err != nil { t.Fatal(err) }
	if !out.Pasted { t.Errorf("default fake should report pasted=true") }
	if len(f.Calls) != 1 || f.Calls[0] != "hello" {
		t.Errorf("Calls = %v", f.Calls)
	}
}

func TestFakeInjectorReturnsNoFocus(t *testing.T) {
	f := &Fake{NextReason: "no-focus"}
	out, _ := f.Inject("x")
	if out.Pasted { t.Errorf("Pasted should be false when reason set") }
	if out.Reason != "no-focus" { t.Errorf("Reason = %q", out.Reason) }
}
