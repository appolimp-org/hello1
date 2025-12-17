package integrationtests

import (
	"common/oyaml"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"gitcore/pkg/pagination"

	"github.com/stretchr/testify/require"
)

type webhookRequest struct {
	Headers http.Header
	Payload map[string]any
}

func (suite *RwApiTestSuite) TestWebhookDispatcher_PushEventDelivery() {
	t := suite.T()
	ctx := context.Background()

	var receivedRequests []webhookRequest
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		require.NoError(t, err)

		receivedRequests = append(receivedRequests, webhookRequest{
			Headers: r.Header,
			Payload: payload,
		})

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// Create repository
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	// Setup git repository with webhook config
	tmpDir := testutils.TempDir(t, "", "webhook-push-test")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.HTTPSProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cg.Must(t, "branch", "-m", "master")

	// Create webhook config
	webhookSlug := "test-webhook-push"
	webhookYAML := fmt.Sprintf(`
webhooks:
  hooks:
    - slug: "test-webhook-push"
      name: Test Push Webhook
      url: %s
      active: true
      ssl_verification: true
  on:
    push:
      - hooks: ["test-webhook-push"]
`, server.URL)

	suite.makeNewFileWithContent(cg, oyaml.WebhooksPath, webhookYAML)
	suite.commitAll(cg, "Add webhook config", "master")

	// Trigger a push event by creating a new commit
	suite.makeNewFileWithContent(cg, "test.txt", "test content")
	suite.commitAll(cg, "Test commit", "master")

	// Wait for webhook workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify webhook was delivered
	require.GreaterOrEqual(t, len(receivedRequests), 1, "Expected at least one webhook delivery")

	// Verify payload structure
	payload := receivedRequests[len(receivedRequests)-1].Payload
	header := payload["header"].(map[string]any)
	require.NotNil(t, header)
	require.NotNil(t, header["id"])
	require.Equal(t, "repository.push", header["type"])
	require.Equal(t, repo.UUID.String(), header["aggregate_id"])
	require.Equal(t, strconv.Itoa(int(repo.OrgID)), header["organization_id"])
	require.NotNil(t, payload["repository"])
	require.NotNil(t, payload["ref_update"])
	require.NotNil(t, payload["pushed_at"])
	require.NotNil(t, payload["default_branch"])
	require.NotNil(t, payload["is_default_branch_updated"])

	// Verify webhook was registered in database
	webhooks, err := suite.WebhookRepo.ListByEntity(ctx, repoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, webhooks.Result, 1)
	require.Equal(t, webhookSlug, webhooks.Result[0].Slug)
	require.Equal(t, server.URL, webhooks.Result[0].PayloadURL)
	require.Equal(t, entities.WebhookStatuses.Active, webhooks.Result[0].Status)

	// Verify webhook log was created
	logs, err := suite.WebhookLogRepo.ListByWebhook(ctx, webhooks.Result[0].ID, pagination.Options{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(logs.Result), 1, "Expected at least one webhook log")
	require.NotNil(t, logs.Result[0].StatusCode)
	require.Equal(t, 200, *logs.Result[0].StatusCode)
}

func (suite *RwApiTestSuite) TestWebhookDispatcher_MultipleWebhooks() {
	t := suite.T()

	// Create two test HTTP servers
	var received1, received2 int
	var mu sync.Mutex

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received1++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received2++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	// Create repository
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	tmpDir := testutils.TempDir(t, "", "webhook-multiple-test")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.HTTPSProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cg.Must(t, "branch", "-m", "master")

	// Create two webhooks
	webhookYAML := fmt.Sprintf(`
webhooks:
  hooks:
    - slug: webhook-1
      name: Webhook 1
      url: %s
      active: true
    - slug: webhook-2
      name: Webhook 2
      url: %s
      active: true
  on:
    push:
      - hooks: [webhook-1, webhook-2]
`, server1.URL, server2.URL)

	suite.makeNewFileWithContent(cg, oyaml.WebhooksPath, webhookYAML)
	suite.commitAll(cg, "Add webhooks", "master")

	// Trigger push event
	suite.makeNewFileWithContent(cg, "test.txt", "test")
	suite.commitAll(cg, "Test commit", "master")

	// Wait for webhook workflows to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify both webhooks were delivered
	require.GreaterOrEqual(t, received1, 1, "Webhook 1 should be delivered")
	require.GreaterOrEqual(t, received2, 1, "Webhook 2 should be delivered")
}

func (suite *RwApiTestSuite) TestWebhookDispatcher_DisabledWebhook() {
	t := suite.T()

	var received int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create repository
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	tmpDir := testutils.TempDir(t, "", "webhook-disabled-test")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.HTTPSProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cg.Must(t, "branch", "-m", "master")

	// Create disabled webhook
	webhookYAML := fmt.Sprintf(`
webhooks:
  hooks:
    - slug: disabled-webhook
      name: Disabled Webhook
      url: %s
      active: false
  on:
    push:
      - hooks: [disabled-webhook]
`, server.URL)

	suite.makeNewFileWithContent(cg, oyaml.WebhooksPath, webhookYAML)
	suite.commitAll(cg, "Add disabled webhook", "master")

	// Trigger push event
	suite.makeNewFileWithContent(cg, "test.txt", "test")
	suite.commitAll(cg, "Test commit", "master")

	// Wait a bit to ensure no delivery happens
	time.Sleep(2 * time.Second)

	// Verify webhook was NOT delivered
	require.Equal(t, 0, received, "Disabled webhook should not be delivered")
}

func (suite *RwApiTestSuite) TestWebhookDispatcher_NoWebhooksConfigured() {
	t := suite.T()
	ctx := context.Background()

	// Create repository without webhook config
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	tmpDir := testutils.TempDir(t, "", "webhook-none-test")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.HTTPSProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cg.Must(t, "branch", "-m", "master")

	// Just push a file without webhook config
	suite.makeNewFileWithContent(cg, "test.txt", "test")
	suite.commitAll(cg, "Test commit", "master")

	// Wait a bit
	time.Sleep(2 * time.Second)

	// Verify no webhooks were created
	webhooks, err := suite.WebhookRepo.ListByEntity(ctx, repoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, webhooks.Result, 0, "No webhooks should be created")
}
