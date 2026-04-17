package agent

import "testing"

func TestStubIdentity(t *testing.T) {
	tests := []struct {
		name string
		stub Stub
		want string
	}{
		{"default", Stub{}, "stub-agent"},
		{"custom", Stub{ID: "a-1"}, "a-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stub.Identity(); got != tt.want {
				t.Fatalf("Identity() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestStubImplementsFacade(t *testing.T) {
	var _ Facade = Stub{}
}
