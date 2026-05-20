package capacity

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"cyber-deploy-hub/internal/contracts/commands"
)

const (
	reservationStateActive   = "ACTIVE"
	reservationStateReleased = "RELEASED"
)

type Snapshot struct {
	Source          string
	VCPUsTotal      int
	VCPUsFree       int
	RAMMiBTotal     int64
	RAMMiBFree      int64
	StorageGiBTotal int64
	StorageGiBUsed  int64
	RawPayload      json.RawMessage
}

type Decision struct {
	Approved         bool
	PredictedCPU     float64
	PredictedRAM     float64
	PredictedStorage float64
	Threshold        float64
	Reason           string
}

func Evaluate(snapshot Snapshot, req commands.LabResourceProfile, threshold float64) Decision {
	decision := Decision{
		Approved:  true,
		Threshold: threshold,
	}

	decision.PredictedCPU = percent(int64(snapshot.VCPUsTotal-snapshot.VCPUsFree+req.VCPU), int64(snapshot.VCPUsTotal))
	decision.PredictedRAM = percent(snapshot.RAMMiBTotal-snapshot.RAMMiBFree+int64(req.RAMMiB), snapshot.RAMMiBTotal)
	decision.PredictedStorage = percent(snapshot.StorageGiBUsed+req.DiskGiB, snapshot.StorageGiBTotal)

	reasons := make([]string, 0, 4)
	if req.VCPU > snapshot.VCPUsFree {
		reasons = append(reasons, "not enough free vcpus")
	}
	if int64(req.RAMMiB) > snapshot.RAMMiBFree {
		reasons = append(reasons, "not enough free ram")
	}
	if req.DiskGiB > snapshot.StorageGiBTotal-snapshot.StorageGiBUsed {
		reasons = append(reasons, "not enough free storage")
	}
	if decision.PredictedCPU > threshold || decision.PredictedRAM > threshold || decision.PredictedStorage > threshold {
		reasons = append(reasons, fmt.Sprintf("predicted utilization exceeds %.2f%% threshold", threshold))
	}
	if len(reasons) > 0 {
		decision.Approved = false
		decision.Reason = strings.Join(reasons, "; ")
	}
	return decision
}

func percent(used int64, total int64) float64 {
	if total <= 0 {
		return 100
	}
	value := (float64(used) / float64(total)) * 100
	return math.Round(value*100) / 100
}
