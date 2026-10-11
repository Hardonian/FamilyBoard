package calendar

import "testing"

func TestCapabilitiesFor_KnownAndUnknown(t *testing.T) {
	if c := CapabilitiesFor("google"); !c.Read || !c.Write || !c.IncrementalSync {
		t.Fatalf("google must be read/write/incremental: %+v", c)
	}
	if c := CapabilitiesFor("apple"); c.IncrementalSync || c.Meetings {
		t.Fatalf("apple must not claim incremental sync or meetings: %+v", c)
	}
	if c := CapabilitiesFor("unknown-provider"); c != (Capabilities{}) {
		t.Fatalf("unknown provider must fail closed: %+v", c)
	}
}

func TestForAccount_ReadOnlySubscription(t *testing.T) {
	c := ForAccount("apple", true)
	if c.Write || !c.Read {
		t.Fatalf("webcal subscription must be read-only: %+v", c)
	}
	if !ForAccount("apple", false).Write {
		t.Fatal("CalDAV account must remain writable")
	}
}

func TestKnownProviders_Sorted(t *testing.T) {
	names := KnownProviders()
	if len(names) < 4 {
		t.Fatalf("expected >=4 providers, got %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("not sorted: %v", names)
		}
	}
}
