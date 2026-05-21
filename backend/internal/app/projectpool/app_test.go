package projectpool

import (
	"testing"

	"cyber-deploy-hub/internal/cloud/openstack"
	"cyber-deploy-hub/internal/config"
)

func TestSeedFromOpenStackProjectUsesScopedProject(t *testing.T) {
	seed := seedFromOpenStackProject(&openstack.ProjectInfo{
		ID:         "b958d8568c81444ba6f408cffbad7d29",
		Name:       "hackhaton_team02",
		DomainName: "Hackhaton",
	}, config.ProjectPoolConfig{
		DefaultCourseID:   "course-3",
		DefaultDomainName: "Hackhaton",
	}, "")

	if len(seed.Domains) != 1 {
		t.Fatalf("domains = %#v", seed.Domains)
	}
	if seed.Domains[0].CourseID != "course-3" {
		t.Fatalf("course id = %q", seed.Domains[0].CourseID)
	}
	if len(seed.Projects) != 1 {
		t.Fatalf("projects = %#v", seed.Projects)
	}
	if seed.Projects[0].ProjectID != "b958d8568c81444ba6f408cffbad7d29" {
		t.Fatalf("project id = %q", seed.Projects[0].ProjectID)
	}
	if seed.Projects[0].DomainID != "Hackhaton" {
		t.Fatalf("domain id = %q", seed.Projects[0].DomainID)
	}
}
