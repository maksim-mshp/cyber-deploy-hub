package labcatalog

import (
	"time"

	"cyber-deploy-hub/internal/contracts/commands"
)

type Definition struct {
	CourseID     string                      `json:"course_id"`
	LabID        string                      `json:"lab_id"`
	Title        string                      `json:"title"`
	Description  string                      `json:"description"`
	Enabled      bool                        `json:"enabled"`
	Resources    commands.LabResourceProfile `json:"resources"`
	Instances    []commands.VMBlueprint      `json:"instances"`
	CheckProfile *commands.CheckerProfileV1  `json:"check_profile,omitempty"`
	UpdatedBy    string                      `json:"updated_by,omitempty"`
	CreatedAt    time.Time                   `json:"created_at,omitempty"`
	UpdatedAt    time.Time                   `json:"updated_at,omitempty"`
}

type ListResult struct {
	Labs []Definition `json:"labs"`
}

type UpdateRequest struct {
	Definition Definition
	ChangedBy  string
}

type UpdateResult struct {
	Lab Definition `json:"lab"`
}
