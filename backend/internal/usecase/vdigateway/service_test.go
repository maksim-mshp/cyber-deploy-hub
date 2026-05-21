package vdigateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cyber-deploy-hub/internal/config"
	"cyber-deploy-hub/internal/contracts"
	"cyber-deploy-hub/internal/contracts/commands"
	"cyber-deploy-hub/internal/contracts/events"
)

func TestServiceIssuesOpaqueAccessURL(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo, fixedTokenGenerator{token: "opaque-token"}, fixedClock{now: testNow})
	envelope := testEnvelope(t, commands.VDIIssueAccessV1, commands.VDIIssueAccessV1Payload{
		LabRunID:  testLabRunID,
		StudentID: "student-1",
		ProjectID: "project-1",
	})

	result, err := service.IssueAccess(context.Background(), envelope)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if result.AccessURL != "https://vdi.example/vdi/session/opaque-token" {
		t.Fatalf("AccessURL = %q", result.AccessURL)
	}
	if repo.issued.ProjectID != "project-1" {
		t.Fatalf("internal project id was not persisted")
	}
	if repo.issued.TokenHash == "opaque-token" {
		t.Fatal("raw token was persisted instead of token hash")
	}
	if repo.event.MessageType != events.VDIAccessIssuedV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}

	var raw map[string]any
	if err := json.Unmarshal(repo.event.Payload, &raw); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if _, ok := raw["project_id"]; ok {
		t.Fatalf("event payload exposes project_id: %#v", raw)
	}
	if got := raw["url"]; got != "https://vdi.example/vdi/session/opaque-token" {
		t.Fatalf("event url = %#v", got)
	}
}

func TestServiceIssueFailureEmitsFailureEvent(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo, fixedTokenGenerator{token: "opaque-token"}, fixedClock{now: testNow})
	envelope := testEnvelope(t, commands.VDIIssueAccessV1, commands.VDIIssueAccessV1Payload{
		LabRunID:  testLabRunID,
		StudentID: "student-1",
	})

	if _, err := service.IssueAccess(context.Background(), envelope); err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if repo.event.MessageType != events.VDIAccessFailedV1.String() || repo.event.Error == nil {
		t.Fatalf("failure event = %#v", repo.event)
	}
	if repo.failureReason != "project_id is required" {
		t.Fatalf("failure reason = %q", repo.failureReason)
	}
}

func TestServiceRevokesFromCommand(t *testing.T) {
	repo := newFakeRepository()
	service := newTestService(t, repo, fixedTokenGenerator{token: "opaque-token"}, fixedClock{now: testNow})
	envelope := testEnvelope(t, commands.VDIRevokeAccessV1, commands.VDIRevokeAccessV1Payload{
		LabRunID: testLabRunID,
		Reason:   "cleanup_due",
	})

	if err := service.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if repo.revokedLabRunID != testLabRunID || repo.revokeReason != "cleanup_due" {
		t.Fatalf("revoke = %q/%q", repo.revokedLabRunID, repo.revokeReason)
	}
	if repo.event.MessageType != events.VDIAccessRevokedV1.String() {
		t.Fatalf("event type = %s", repo.event.MessageType)
	}
}

func TestServiceOpensActiveSession(t *testing.T) {
	repo := newFakeRepository()
	hash, err := TokenHash("opaque-token")
	if err != nil {
		t.Fatalf("TokenHash: %v", err)
	}
	repo.tokens[hash] = AccessToken{
		TokenHash: hash,
		LabRunID:  testLabRunID,
		StudentID: "student-1",
		ProjectID: "project-1",
		State:     TokenStateActive,
		ExpiresAt: testNow.Add(time.Minute),
		IssuedAt:  testNow,
	}
	service := newTestService(t, repo, fixedTokenGenerator{token: "unused"}, fixedClock{now: testNow})

	launch, err := service.OpenSession(context.Background(), OpenSessionRequest{
		Token:      "opaque-token",
		RemoteAddr: "127.0.0.1",
		UserAgent:  "test-agent",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if launch.LaunchURL != "/novnc?session=opaque-token" {
		t.Fatalf("LaunchURL = %q", launch.LaunchURL)
	}
	if repo.openedTokenHash != hash || repo.openedRemoteAddr != "127.0.0.1" {
		t.Fatalf("open audit = %q/%q", repo.openedTokenHash, repo.openedRemoteAddr)
	}
}

func TestServiceOpensTargetedInstanceSession(t *testing.T) {
	repo := newFakeRepository()
	hash, err := TokenHash("opaque-token")
	if err != nil {
		t.Fatalf("TokenHash: %v", err)
	}
	repo.tokens[hash] = AccessToken{
		TokenHash: hash,
		LabRunID:  testLabRunID,
		StudentID: "student-1",
		ProjectID: "project-1",
		State:     TokenStateActive,
		ExpiresAt: testNow.Add(time.Minute),
		IssuedAt:  testNow,
	}
	repo.instances["server-1"] = InstanceTarget{
		ServerID: "server-1",
		Name:     "L-MS",
		State:    "ACTIVE",
	}
	service := newTestService(t, repo, fixedTokenGenerator{token: "unused"}, fixedClock{now: testNow})

	launch, err := service.OpenSession(context.Background(), OpenSessionRequest{
		Token:    "opaque-token",
		ServerID: "server-1",
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if launch.LaunchURL != "/novnc?instance_name=L-MS&server_id=server-1&session=opaque-token" {
		t.Fatalf("LaunchURL = %q", launch.LaunchURL)
	}
	if launch.ServerID != "server-1" || launch.InstanceName != "L-MS" {
		t.Fatalf("launch target = %#v", launch)
	}
}

func TestServiceRejectsUnknownInstanceTarget(t *testing.T) {
	repo := newFakeRepository()
	hash, err := TokenHash("opaque-token")
	if err != nil {
		t.Fatalf("TokenHash: %v", err)
	}
	repo.tokens[hash] = AccessToken{
		TokenHash: hash,
		LabRunID:  testLabRunID,
		StudentID: "student-1",
		ProjectID: "project-1",
		State:     TokenStateActive,
		ExpiresAt: testNow.Add(time.Minute),
		IssuedAt:  testNow,
	}
	service := newTestService(t, repo, fixedTokenGenerator{token: "unused"}, fixedClock{now: testNow})

	_, err = service.OpenSession(context.Background(), OpenSessionRequest{
		Token:    "opaque-token",
		ServerID: "missing-server",
	})
	if !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("OpenSession err = %v, want ErrInstanceNotFound", err)
	}
}

func TestServiceExpiresSessionToken(t *testing.T) {
	repo := newFakeRepository()
	hash, err := TokenHash("opaque-token")
	if err != nil {
		t.Fatalf("TokenHash: %v", err)
	}
	repo.tokens[hash] = AccessToken{
		TokenHash: hash,
		LabRunID:  testLabRunID,
		StudentID: "student-1",
		ProjectID: "project-1",
		State:     TokenStateActive,
		ExpiresAt: testNow.Add(-time.Second),
		IssuedAt:  testNow.Add(-time.Hour),
	}
	service := newTestService(t, repo, fixedTokenGenerator{token: "unused"}, fixedClock{now: testNow})

	_, err = service.OpenSession(context.Background(), OpenSessionRequest{Token: "opaque-token"})
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("OpenSession err = %v, want ErrTokenExpired", err)
	}
	if repo.expiredTokenHash != hash {
		t.Fatalf("expired token hash = %q", repo.expiredTokenHash)
	}
}

type fakeRepository struct {
	issued           AccessToken
	tokens           map[string]AccessToken
	instances        map[string]InstanceTarget
	event            contracts.Envelope
	failureReason    string
	revokedLabRunID  string
	revokeReason     string
	expiredTokenHash string
	openedTokenHash  string
	openedRemoteAddr string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		tokens:    map[string]AccessToken{},
		instances: map[string]InstanceTarget{},
	}
}

func (r *fakeRepository) SaveIssued(_ context.Context, _ contracts.Envelope, token AccessToken, event contracts.Envelope) error {
	r.issued = token
	r.tokens[token.TokenHash] = token
	r.event = event
	return nil
}

func (r *fakeRepository) SaveIssueFailure(_ context.Context, _ contracts.Envelope, _ string, _ string, _ string, reason string, event contracts.Envelope) error {
	r.failureReason = reason
	r.event = event
	return nil
}

func (r *fakeRepository) RevokeByLabRun(_ context.Context, _ contracts.Envelope, labRunID string, reason string, event contracts.Envelope) error {
	r.revokedLabRunID = labRunID
	r.revokeReason = reason
	r.event = event
	return nil
}

func (r *fakeRepository) FindToken(_ context.Context, tokenHash string) (AccessToken, bool, error) {
	token, ok := r.tokens[tokenHash]
	return token, ok, nil
}

func (r *fakeRepository) FindInstanceTarget(_ context.Context, _ string, serverID string) (InstanceTarget, bool, error) {
	target, ok := r.instances[serverID]
	return target, ok, nil
}

func (r *fakeRepository) MarkExpired(_ context.Context, tokenHash string) error {
	r.expiredTokenHash = tokenHash
	return nil
}

func (r *fakeRepository) RecordOpen(_ context.Context, tokenHash string, _ string, _ time.Time, remoteAddr string, _ string) error {
	r.openedTokenHash = tokenHash
	r.openedRemoteAddr = remoteAddr
	return nil
}

type fixedTokenGenerator struct {
	token string
	err   error
}

func (g fixedTokenGenerator) Generate() (string, error) {
	return g.token, g.err
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func newTestService(t *testing.T, repo Repository, generator TokenGenerator, clk clock) *Service {
	t.Helper()
	service, err := NewServiceWithDeps("vdi-gateway-service", repo, config.VDIConfig{
		PublicBaseURL:      "https://vdi.example",
		AccessTokenTTL:     10 * time.Minute,
		ConsoleURLTemplate: "/novnc?session={token}",
	}, generator, clk)
	if err != nil {
		t.Fatalf("NewServiceWithDeps: %v", err)
	}
	return service
}

func testEnvelope(t *testing.T, subject contracts.Subject, payload any) contracts.Envelope {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return contracts.Envelope{
		MessageID:      "11111111-1111-4111-8111-111111111111",
		MessageKind:    contracts.MessageKindCommand,
		MessageType:    subject.String(),
		SchemaVersion:  1,
		Producer:       "test",
		CorrelationID:  "11111111-1111-4111-8111-111111111111",
		SagaID:         "22222222-2222-4222-8222-222222222222",
		AggregateType:  "lab_run",
		AggregateID:    testLabRunID,
		IdempotencyKey: "test:" + subject.String(),
		Payload:        rawPayload,
	}
}

var (
	testNow      = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	testLabRunID = "33333333-3333-4333-8333-333333333333"
)
