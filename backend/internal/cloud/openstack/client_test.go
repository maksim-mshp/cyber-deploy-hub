package openstack

import (
	"testing"

	"cyber-deploy-hub/internal/config"
)

func TestProjectScopedConfigUsesAllocatedProject(t *testing.T) {
	client := NewClient(config.OpenStackConfig{
		AuthURL:           "https://openstack.example:5000/v3",
		Username:          "svc",
		Password:          "secret",
		UserDomainName:    "Hackhaton",
		ProjectName:       "service-project",
		ProjectDomainName: "Hackhaton",
		Region:            "RegionOne",
	})

	scoped, err := client.projectScopedConfig(" 11111111-1111-4111-8111-111111111111 ")
	if err != nil {
		t.Fatalf("projectScopedConfig: %v", err)
	}
	if scoped.ProjectID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("project_id = %q", scoped.ProjectID)
	}
	if scoped.ProjectName != "" {
		t.Fatalf("project_name must be cleared for project_id scope, got %q", scoped.ProjectName)
	}
	if !scoped.Configured() {
		t.Fatal("scoped config must be usable for project-scoped auth")
	}
}

func TestProjectScopedConfigRequiresProjectID(t *testing.T) {
	client := NewClient(config.OpenStackConfig{
		AuthURL:  "https://openstack.example:5000/v3",
		Username: "svc",
		Password: "secret",
	})

	if _, err := client.projectScopedConfig(" "); err == nil {
		t.Fatal("expected empty project id to be rejected")
	}
}
