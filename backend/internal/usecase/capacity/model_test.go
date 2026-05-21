package capacity

import (
	"strings"
	"testing"

	"cyber-deploy-hub/internal/contracts/commands"
)

func TestEvaluateApprovesUnderThreshold(t *testing.T) {
	decision := Evaluate(Snapshot{
		VCPUsTotal:      100,
		VCPUsFree:       60,
		RAMMiBTotal:     1000,
		RAMMiBFree:      600,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  300,
	}, commands.LabResourceProfile{
		VCPU:    10,
		RAMMiB:  100,
		DiskGiB: 100,
	}, 90)

	if !decision.Approved {
		t.Fatalf("decision denied: %s", decision.Reason)
	}
	if decision.PredictedCPU != 50 {
		t.Fatalf("predicted cpu = %v, want 50", decision.PredictedCPU)
	}
	if decision.PredictedRAM != 50 {
		t.Fatalf("predicted ram = %v, want 50", decision.PredictedRAM)
	}
	if decision.PredictedStorage != 40 {
		t.Fatalf("predicted storage = %v, want 40", decision.PredictedStorage)
	}
}

func TestEvaluateDeniesAboveThreshold(t *testing.T) {
	decision := Evaluate(Snapshot{
		VCPUsTotal:      100,
		VCPUsFree:       20,
		RAMMiBTotal:     1000,
		RAMMiBFree:      600,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  300,
	}, commands.LabResourceProfile{
		VCPU:    15,
		RAMMiB:  100,
		DiskGiB: 100,
	}, 90)

	if decision.Approved {
		t.Fatal("expected denial")
	}
	if !strings.Contains(decision.Reason, "predicted utilization exceeds") {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestEvaluateDeniesWhenFreeResourcesInsufficient(t *testing.T) {
	decision := Evaluate(Snapshot{
		VCPUsTotal:      100,
		VCPUsFree:       8,
		RAMMiBTotal:     1000,
		RAMMiBFree:      50,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  980,
	}, commands.LabResourceProfile{
		VCPU:    10,
		RAMMiB:  100,
		DiskGiB: 100,
	}, 99)

	if decision.Approved {
		t.Fatal("expected denial")
	}
	for _, part := range []string{"not enough free vcpus", "not enough free ram", "not enough free storage"} {
		if !strings.Contains(decision.Reason, part) {
			t.Fatalf("reason %q missing %q", decision.Reason, part)
		}
	}
}

func TestEvaluateHandlesFreeCapacityGreaterThanTotal(t *testing.T) {
	decision := Evaluate(Snapshot{
		VCPUsTotal:      16,
		VCPUsFree:       1904,
		RAMMiBTotal:     1000,
		RAMMiBFree:      1200,
		StorageGiBTotal: 1000,
		StorageGiBUsed:  100,
	}, commands.LabResourceProfile{
		VCPU:    8,
		RAMMiB:  100,
		DiskGiB: 100,
	}, 90)

	if !decision.Approved {
		t.Fatalf("decision denied: %s", decision.Reason)
	}
	if decision.PredictedCPU != 50 {
		t.Fatalf("predicted cpu = %v, want 50", decision.PredictedCPU)
	}
	if decision.PredictedRAM != 10 {
		t.Fatalf("predicted ram = %v, want 10", decision.PredictedRAM)
	}
}
