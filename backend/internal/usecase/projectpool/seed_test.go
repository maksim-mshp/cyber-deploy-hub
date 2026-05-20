package projectpool

import "testing"

func TestParseSeedValidatesDomainsAndProjects(t *testing.T) {
	seed, err := ParseSeed([]byte(`{
		"domains": [{"domain_id": "course-linux", "course_id": "course-1", "name": "Linux"}],
		"projects": [{"project_id": "11111111-1111-4111-8111-111111111111", "domain_id": "course-linux", "name": "linux-001"}]
	}`))
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}
	if len(seed.Domains) != 1 || len(seed.Projects) != 1 {
		t.Fatalf("seed = %#v", seed)
	}
}

func TestParseSeedRejectsUnknownDomain(t *testing.T) {
	_, err := ParseSeed([]byte(`{
		"domains": [{"domain_id": "course-linux", "course_id": "course-1", "name": "Linux"}],
		"projects": [{"project_id": "11111111-1111-4111-8111-111111111111", "domain_id": "missing", "name": "linux-001"}]
	}`))
	if err == nil {
		t.Fatal("expected unknown domain error")
	}
}

func TestParseSeedRejectsInvalidProjectID(t *testing.T) {
	_, err := ParseSeed([]byte(`{
		"domains": [{"domain_id": "course-linux", "course_id": "course-1", "name": "Linux"}],
		"projects": [{"project_id": "not-a-uuid", "domain_id": "course-linux", "name": "linux-001"}]
	}`))
	if err == nil {
		t.Fatal("expected invalid project id error")
	}
}
