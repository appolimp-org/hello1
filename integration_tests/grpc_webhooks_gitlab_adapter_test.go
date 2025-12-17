package integrationtests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/webhook/gitlab"
	"gitcore/internal/testutils"

	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
)

// capturedWebhook holds captured webhook delivery data
type capturedWebhook struct {
	Headers http.Header
	Body    []byte
}

type testWebhookSever struct {
	Server     *httptest.Server
	Deliveries []capturedWebhook
	Enabled    bool
}

func (suite *RwApiTestSuite) createTestWebhookServer() *testWebhookSever {
	testWebhookSever := &testWebhookSever{
		Deliveries: []capturedWebhook{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !testWebhookSever.Enabled {
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("ok"))
			return
		}

		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		captured := capturedWebhook{
			Headers: r.Header.Clone(),
			Body:    body,
		}
		testWebhookSever.Deliveries = append(testWebhookSever.Deliveries, captured)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("ok"))
	}))
	testWebhookSever.Server = server
	return testWebhookSever
}

func (ws *testWebhookSever) Close() {
	ws.Server.Close()
}

// verifyGitLabHeaders verifies that GitLab-specific headers are present and valid
func (suite *RwApiTestSuite) verifyGitLabHeaders(t *testing.T, headers http.Header) {
	require.NotEmpty(t, headers.Get("X-Gitlab-Event"), "X-Gitlab-Event should be present")
	require.NotEmpty(t, headers.Get("X-Gitlab-Event-Uuid"), "X-Gitlab-Event-UUID should be present")
	require.NotEmpty(t, headers.Get("X-Gitlab-Webhook-Uuid"), "X-Gitlab-Webhook-UUID should be present")
	require.NotEmpty(t, headers.Get("X-Gitlab-Instance"), "X-Gitlab-Instance should be present")
	require.NotEmpty(t, headers.Get("X-Gitlab-Token"), "X-Gitlab-Token should be present")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GitLabAdapter_SingleBranchPush() {
	t := suite.T()

	// Create mock webhook server that captures all deliveries
	ts := suite.createTestWebhookServer()
	defer ts.Close()

	// Add webhooks config with -experimenal_use_gitlab_adapter suffix to trigger GitLab adapter
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: push-webhook-experimenal_use_gitlab_adapter
      name: "GitLab Push Webhook"
      url: "` + ts.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [push-webhook-experimenal_use_gitlab_adapter]
`)

	ts.Enabled = true
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single push delivery")
	delivery := ts.Deliveries[0]

	// Verify GitLab-specific headers
	suite.verifyGitLabHeaders(t, delivery.Headers)
	require.Equal(t, "Push Hook", delivery.Headers.Get("X-Gitlab-Event"))

	// Verify payload structure is GitLab-formatted
	var payload gitlab.PushEventPayload
	err := json.Unmarshal(delivery.Body, &payload)
	require.NoError(t, err, "payload should be valid GitLab push event")

	// Verify GitLab payload fields
	require.Equal(t, "push", payload.ObjectKind)
	require.Equal(t, "refs/heads/master", payload.Ref)
	require.NotEmpty(t, payload.Before, "before SHA should be present")
	require.NotEmpty(t, payload.After, "after SHA should be present")
	require.NotEmpty(t, payload.CheckoutSHA, "checkout SHA should be present")
	require.Equal(t, int64(wt.RepoID), payload.ProjectID)

	// Verify user information
	require.NotEmpty(t, payload.UserUsername)
	require.NotZero(t, payload.UserID)

	// Verify project information
	require.Equal(t, wt.RepoSlug, payload.Project.Name)
	require.Equal(t, wt.OrgSlug+"/"+wt.RepoSlug, payload.Project.PathWithNamespace)
	require.NotEmpty(t, payload.Project.GitSSHURL)
	require.NotEmpty(t, payload.Project.GitHTTPURL)
	require.NotEmpty(t, payload.Project.WebURL)

	// Verify repository information
	require.Equal(t, wt.RepoSlug, payload.Repository.Name)
	require.NotEmpty(t, payload.Repository.GitSSHURL)
	require.NotEmpty(t, payload.Repository.GitHTTPURL)

	// Verify X-Gitlab-Token uses sign_key (not the secret field)
	gitlabToken := delivery.Headers.Get("X-Gitlab-Token")
	require.NotEmpty(t, gitlabToken)

	// Get the webhook's sign_key to verify
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, resp.Webhooks[0].SignKey, gitlabToken, "X-Gitlab-Token should use sign_key")

	// Verify commits
	// require.Len(t, payload.Commits, 1)
	// require.Equal(t, "trigger webhook on master", payload.Commits[0].Title)
	// require.NotEmpty(t, payload.Commits[0].ID)
	// require.Equal(t, int64(1), payload.TotalCommitsCount)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GitLabAdapter_MultipleBranchPush() {
	t := suite.T()

	// Create mock webhook server that captures all deliveries
	ts := suite.createTestWebhookServer()
	defer ts.Close()

	// Add webhooks config with -experimenal_use_gitlab_adapter suffix to trigger GitLab adapter
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: push-webhook-experimenal_use_gitlab_adapter
      name: "GitLab Push Webhook"
      url: "` + ts.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [push-webhook-experimenal_use_gitlab_adapter]
`)

	ts.Enabled = true

	// Create multiple branches with commits
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit on master for multi-branch test")

	// Create branch1
	wt.Cgit.Must(t, "checkout", "-b", "branch1")
	suite.makeNewFileWithContent(wt.Cgit, "file2.txt", "file2")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit on branch1")

	// Create branch2
	wt.Cgit.Must(t, "checkout", "master")
	wt.Cgit.Must(t, "checkout", "-b", "branch2")
	suite.makeNewFileWithContent(wt.Cgit, "file3.txt", "file3")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit on branch2")

	// Create branch3
	wt.Cgit.Must(t, "checkout", "master")
	wt.Cgit.Must(t, "checkout", "-b", "branch3")
	suite.makeNewFileWithContent(wt.Cgit, "file4.txt", "file4")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit on branch3")

	// Push all branches at once to trigger multiple ref updates
	wt.Cgit.Must(t, "push", "origin", "master", "branch1", "branch2", "branch3")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify multiple deliveries were made (one per ref/branch)
	require.Len(t, ts.Deliveries, 4, "should have 4 deliveries (one per branch)")

	// Track which branches we received deliveries for
	branchesReceived := make(map[string]bool)
	var payloads []gitlab.PushEventPayload

	for _, delivery := range ts.Deliveries {
		// Verify GitLab headers on each delivery
		suite.verifyGitLabHeaders(t, delivery.Headers)
		require.Equal(t, "Push Hook", delivery.Headers.Get("X-Gitlab-Event"))

		// Parse payload
		var payload gitlab.PushEventPayload
		err := json.Unmarshal(delivery.Body, &payload)
		require.NoError(t, err, "each payload should be valid GitLab push event")

		// Track branch
		if after, ok := strings.CutPrefix(payload.Ref, "refs/heads/"); ok {
			branchName := after
			branchesReceived[branchName] = true
		}

		// Verify common GitLab payload structure
		require.Equal(t, "push", payload.ObjectKind)
		require.Equal(t, int64(wt.RepoID), payload.ProjectID)
		require.NotEmpty(t, payload.After)
		require.NotEmpty(t, payload.CheckoutSHA)

		// Verify project info is present in each delivery
		require.Equal(t, wt.RepoSlug, payload.Project.Name)
		require.Equal(t, wt.OrgSlug+"/"+wt.RepoSlug, payload.Project.PathWithNamespace)

		payloads = append(payloads, payload)
	}

	// Verify we received deliveries for all pushed branches
	expectedBranches := []string{"master", "branch1", "branch2", "branch3"}
	for _, branch := range expectedBranches {
		require.True(t, branchesReceived[branch], "should receive delivery for branch: %s", branch)
	}

	// Verify each delivery has different ref
	uniqueRefs := make(map[string]bool)
	for _, payload := range payloads {
		uniqueRefs[payload.Ref] = true
	}
	require.Equal(t, len(uniqueRefs), 4, "should have deliveries for different refs")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GitLabAdapter_SingleTagPush() {
	t := suite.T()

	// Create mock webhook server that captures all deliveries
	ts := suite.createTestWebhookServer()
	defer ts.Close()

	// Add webhooks config with -experimenal_use_gitlab_adapter suffix to trigger GitLab adapter
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: push-webhook-experimenal_use_gitlab_adapter
      name: "GitLab Push Webhook"
      url: "` + ts.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [push-webhook-experimenal_use_gitlab_adapter]
`)

	ts.Enabled = true
	wt.Cgit.Must(t, "tag", "-a", "tag1", "-m", "add tag1")
	wt.Cgit.Must(t, "push", "origin", "tag1")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single push delivery")
	delivery := ts.Deliveries[0]

	// Verify GitLab-specific headers
	suite.verifyGitLabHeaders(t, delivery.Headers)
	require.Equal(t, "Tag Push Hook", delivery.Headers.Get("X-Gitlab-Event"))

	// Verify payload structure is GitLab-formatted
	var payload gitlab.TagEventPayload
	err := json.Unmarshal(delivery.Body, &payload)
	require.NoError(t, err, "payload should be valid GitLab tag push event")

	// Verify GitLab payload fields
	require.Equal(t, "tag_push", payload.ObjectKind)
	require.Equal(t, "refs/tags/tag1", payload.Ref)
	require.NotEmpty(t, payload.Before, "before SHA should be present")
	require.NotEmpty(t, payload.After, "after SHA should be present")
	require.NotEmpty(t, payload.CheckoutSHA, "checkout SHA should be present")
	require.Equal(t, int64(wt.RepoID), payload.ProjectID)

	// Verify user information
	require.NotEmpty(t, payload.UserUsername)
	require.NotZero(t, payload.UserID)

	// Verify project information
	require.Equal(t, wt.RepoSlug, payload.Project.Name)
	require.Equal(t, wt.OrgSlug+"/"+wt.RepoSlug, payload.Project.PathWithNamespace)
	require.NotEmpty(t, payload.Project.GitSSHURL)
	require.NotEmpty(t, payload.Project.GitHTTPURL)
	require.NotEmpty(t, payload.Project.WebURL)

	// Verify repository information
	require.Equal(t, wt.RepoSlug, payload.Repository.Name)
	require.NotEmpty(t, payload.Repository.GitSSHURL)
	require.NotEmpty(t, payload.Repository.GitHTTPURL)

	// Verify X-Gitlab-Token uses sign_key (not the secret field)
	gitlabToken := delivery.Headers.Get("X-Gitlab-Token")
	require.NotEmpty(t, gitlabToken)

	// Get the webhook's sign_key to verify
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, resp.Webhooks[0].SignKey, gitlabToken, "X-Gitlab-Token should use sign_key")

	// Verify commits
	// require.Len(t, payload.Commits, 1)
	// require.Equal(t, "trigger webhook on master", payload.Commits[0].Title)
	// require.NotEmpty(t, payload.Commits[0].ID)
	// require.Equal(t, int64(1), payload.TotalCommitsCount)
}
