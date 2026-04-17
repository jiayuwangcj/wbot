package master

import (
	"sort"
	"testing"
)

func TestMemoryRegister(t *testing.T) {
	m := NewMemory()
	if m.Register("") {
		t.Fatal("empty id should not register")
	}
	if !m.Register("x") {
		t.Fatal("first register should be new")
	}
	if m.Register("x") {
		t.Fatal("duplicate should not be new")
	}
	got := m.Agents()
	sort.Strings(got)
	want := []string{"x"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Agents() = %v; want %v", got, want)
	}
}

func TestMemoryImplementsFacade(t *testing.T) {
	var _ Facade = NewMemory()
}
