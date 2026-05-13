package attributionlocal

import "testing"

func TestScanner_SkipsAlreadyWatermarkedCodexRows(t *testing.T) {
	fixture := buildAttributionFixture(t)
	scanner := NewScanner()

	first, state, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected first scan events")
	}

	second, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, state)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second scan events = %d, want 0", len(second))
	}
}
