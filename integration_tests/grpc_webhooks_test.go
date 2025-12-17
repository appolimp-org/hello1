package integrationtests

import (
	"common/cgit"
	"common/oyaml"
	"common/utils"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"sort"
	"time"

	"gitcore/pkg/pagination"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/stretchr/testify/require"

	page "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

type webhookTestInfo struct {
	RepoID   uint64
	OrgSlug  string
	RepoSlug string
	Org      *entities.Organization
	Cgit     cgit.CGit
}

func (suite *RwApiTestSuite) prepareWebhooksTestWithConfig(webhookYaml string) *webhookTestInfo {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
	_, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	org, err := suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	protocol := suite.HTTPSProtocol()
	cgAdmin := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cgAdmin.Must(t, "init", ".")
	cgAdmin.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cgAdmin.Must(t, "branch", "-m", "master")

	configYaml := strings.ReplaceAll(webhookYaml, "%s", server.URL)
	suite.makeNewFileWithContent(cgAdmin, oyaml.WebhooksPath, configYaml)
	suite.commitAll(cgAdmin, "add config", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	return &webhookTestInfo{
		RepoID:   repoID,
		OrgSlug:  orgSlug,
		RepoSlug: repoSlug,
		Org:      org,
		Cgit:     cgAdmin,
	}
}

func (suite *RwApiTestSuite) changeWebhooksTestConfig(wt *webhookTestInfo, webhookYaml string) {
	t := suite.T()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	configYaml := strings.ReplaceAll(webhookYaml, "%s", server.URL)
	suite.makeNewFileWithContent(wt.Cgit, oyaml.WebhooksPath, configYaml)
	suite.commitAll(wt.Cgit, "change webhooks config", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Triggers on main branch pushes"
      url: "%s"
      secret: "my-secret"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Len(t, resp.Webhooks[0].Events, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, "Webhook1", *resp.Webhooks[0].Name)
	require.Equal(t, "push", resp.Webhooks[0].Events[0])
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_EmptyWebhooks() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks: []
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 0)
	require.NotNil(t, resp.ConfigAsCode)
	require.NotNil(t, resp.ConfigAsCode.Commit)
	require.False(t, resp.ConfigAsCode.CorruptConfig)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_MultipleWebhooks() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Triggers on push"
      url: "%s/webhook1"
      secret: "secret1"
      ssl_verification: true
      active: true
    - slug: wh2
      name: "Webhook2"
      description: "Triggers on refs update"
      url: "%s/webhook2"
      secret: "secret2"
      ssl_verification: false
      active: false
    - slug: wh3
      name: "Webhook3"
      description: "Triggers on multiple events"
      url: "%s/webhook3"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1, wh3]
    refs_update:
      - hooks: [wh2, wh3]
    repository.refs_update:
      - hooks: [wh2]
    repository.push:
      - hooks: [wh3]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*page.SortOption{
			{
				Column:    "slug",
				Direction: page.SortOption_ASC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)

	// Verify first webhook
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, "Webhook1", *resp.Webhooks[0].Name)
	require.Len(t, resp.Webhooks[0].Events, 1)
	require.Equal(t, "push", resp.Webhooks[0].Events[0])
	require.True(t, resp.Webhooks[0].SslVerification)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, resp.Webhooks[0].Status)

	// Verify second webhook
	require.Equal(t, "wh2", resp.Webhooks[1].Slug)
	require.Equal(t, "Webhook2", *resp.Webhooks[1].Name)
	require.Len(t, resp.Webhooks[1].Events, 2)
	require.Contains(t, resp.Webhooks[1].Events, "refs_update")
	require.Contains(t, resp.Webhooks[1].Events, "repository.refs_update")
	require.False(t, resp.Webhooks[1].SslVerification)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, resp.Webhooks[1].Status)

	// Verify third webhook
	require.Equal(t, "wh3", resp.Webhooks[2].Slug)
	require.Equal(t, "Webhook3", *resp.Webhooks[2].Name)
	require.Len(t, resp.Webhooks[2].Events, 3)
	require.Contains(t, resp.Webhooks[2].Events, "push")
	require.Contains(t, resp.Webhooks[2].Events, "refs_update")
	require.Contains(t, resp.Webhooks[2].Events, "repository.push")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_CorruptConfig() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: [invalid: yaml: structure}
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.ConfigAsCode)
	require.True(t, resp.ConfigAsCode.CorruptConfig)
	require.NotEmpty(t, resp.ConfigAsCode.Error)
	require.NotNil(t, resp.ConfigAsCode.Commit)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_MissingRepoID() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: "",
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "repo_id")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_UnauthorizedAccess() {
	t := suite.T()

	// Create repo with Kopatych as owner
	repoID, orgSlug, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	_, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	_, err = suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	// Try to access with different user (Krosh) who doesn't have permissions
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(repoID),
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetDeliveries() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Test webhook"
      url: "%s"
      secret: "my-secret"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.GetDeliveries(ctx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Test GetDeliveries for a webhook that exists in config but not in DB yet
	// Webhooks are only persisted to DB when they're first triggered
	resp2, err := client.GetDeliveries(ctx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh2",
	})
	require.Error(t, err)
	require.Nil(t, resp2)
	require.Contains(t, err.Error(), "wh2")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetDeliveries_MissingRepoID() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.GetDeliveries(ctx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: "",
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "repo_id")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetDeliveries_WebhookNotFound() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Try to get deliveries for a non-existent webhook slug
	resp, err := client.GetDeliveries(ctx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "nonexistent",
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetDeliveries_UnauthorizedAccess() {
	t := suite.T()

	// Create repo with Kopatych as owner
	repoID, orgSlug, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	_, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	_, err = suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	// Try to access with different user (Krosh) who doesn't have permissions
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.GetDeliveries(ctx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(repoID),
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetDeliveries_WithDeliveriesInDB() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Test webhook"
      url: "%s"
      secret: "my-secret"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := context.Background()

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp.Webhooks[0].Status)

	webhookID, err := grpc_marshalling.IDDirect(listResp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Create webhook delivery logs
	eventID1 := "event123"
	statusCode200 := 200
	responseBody := "OK"
	log1 := &entities.WebhookLog{
		WebhookID:    webhookID,
		EventID:      &eventID1,
		StatusCode:   &statusCode200,
		ResponseBody: &responseBody,
		AttemptCount: 1,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log1)
	require.NoError(t, err)

	eventID2 := "event456"
	statusCode500 := 500
	errorMsg := "Internal Server Error"
	log2 := &entities.WebhookLog{
		WebhookID:    webhookID,
		EventID:      &eventID2,
		StatusCode:   &statusCode500,
		AttemptCount: 2,
		Error:        &errorMsg,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log2)
	require.NoError(t, err)

	// Get deliveries
	resp, err := client.GetDeliveries(grpcCtx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Deliveries, 3)

	// Verify deliveries are returned (most recent first by default)
	// Note: The order depends on the paginator sort order
	found200 := false
	found500 := false
	for _, delivery := range resp.Deliveries {
		if delivery.StatusCode == 200 {
			found200 = true
			require.Equal(t, eventID1, delivery.EventId)
			require.Equal(t, "OK", delivery.ResponseBody)
			require.Equal(t, int32(1), delivery.AttemptCount)
		}
		if delivery.StatusCode == 500 {
			found500 = true
			require.Equal(t, eventID2, delivery.EventId)
			require.Equal(t, int32(2), delivery.AttemptCount)
			require.Equal(t, errorMsg, delivery.Error)
		}
	}
	require.True(t, found200, "Expected to find delivery with status code 200")
	require.True(t, found500, "Expected to find delivery with status code 500")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Test webhook"
      url: "%s"
      secret: "my-secret"
      ssl_verification: true
      active: false
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First, list webhooks to verify it exists in config and is disabled
	listResp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, listResp.Webhooks[0].Status)

	// Try to enable the webhook - should fail because webhook doesn't exist in DB yet
	// Webhooks are only persisted to DB when they're first triggered/used
	enableResp, err := client.Enable(ctx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, enableResp)
	require.Contains(t, err.Error(), "wh1")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_WithWebhookInDB() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Test webhook"
      url: "%s"
      secret: "my-secret"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := context.Background()

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First, list webhooks to verify it exists
	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp.Webhooks[0].Status)

	// Create a webhook in the database with disabled status
	webhook := &entities.Webhook{
		Slug:            "wh1",
		EntityType:      entities.EntityTypes.Repository,
		EntityID:        wt.RepoID,
		PayloadURL:      "https://example.com/webhook",
		Status:          entities.WebhookStatuses.Disabled,
		SSLVerification: true,
		Events:          entities.WebhookEvents{"push"},
	}
	webhookID, err := suite.WebhookRepo.CreateOrGet(ctx, webhook)
	require.NoError(t, err)
	require.NotZero(t, webhookID)

	err = suite.WebhookRepo.Update(webhookID).SetStatus(entities.WebhookStatuses.Disabled).Commit(ctx)
	require.NoError(t, err)

	// First, list webhooks to verify it is disabled
	listResp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_DISABLED, listResp.Webhooks[0].Status)

	// Enable the webhook
	enableResp, err := client.Enable(grpcCtx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	require.NotNil(t, enableResp)

	// Verify webhook is now enabled by listing again
	listResp2, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp2.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp2.Webhooks[0].Status)

	// Verify in database directly
	updatedWebhook, err := suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, entities.WebhookStatuses.Active, updatedWebhook.Status)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_AlreadyActiveInDB() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := context.Background()

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First, list webhooks to verify it's already active
	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp.Webhooks[0].Status)

	webhookID, err := grpc_marshalling.IDDirect(listResp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Enable the already-active webhook (should succeed as idempotent operation)
	enableResp, err := client.Enable(grpcCtx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	require.NotNil(t, enableResp)

	// Verify webhook is still active
	listResp2, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp2.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp2.Webhooks[0].Status)

	// Verify in database directly that it's still active
	updatedWebhook, err := suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, entities.WebhookStatuses.Active, updatedWebhook.Status)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_MissingRepoID() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.Enable(ctx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: "",
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "repo_id")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_WebhookNotFound() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: false
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Try to enable a non-existent webhook slug
	resp, err := client.Enable(ctx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "nonexistent",
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_UnauthorizedAccess() {
	t := suite.T()

	// Create repo with Kopatych as owner
	repoID, orgSlug, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	_, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	_, err = suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	// Try to access with different user (Krosh) who doesn't have permissions
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.Enable(ctx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(repoID),
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Enable_WebhookNotInDatabase() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First, list webhooks to verify it exists in config and is active
	listResp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, listResp.Webhooks[0].Status)

	// Try to enable the webhook - should fail because webhook doesn't exist in DB yet
	// Even though it's active in config, Enable requires a DB record
	enableResp, err := client.Enable(ctx, &pb.EnableWebhookRequest{
		Entity: &pb.EnableWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh2",
	})
	require.Error(t, err)
	require.Nil(t, enableResp)
	require.Contains(t, err.Error(), "wh2")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_DeletedWebhookFromConfig() {
	t := suite.T()
	ctx := context.Background()

	// Create initial config with two webhooks
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
    - slug: wh2
      name: "Webhook2"
      url: "%s/webhook2"
      active: true
  on:
    push:
      - hooks: [wh1]
    refs_update:
      - hooks: [wh2]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First List call will create webhooks in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 2)

	// Get webhook IDs from response
	var wh1ID, wh2ID uint64
	for _, webhook := range resp.Webhooks {
		switch webhook.Slug {
		case "wh1":
			wh1ID, _ = grpc_marshalling.IDDirect(webhook.WebhookId)
		case "wh2":
			wh2ID, _ = grpc_marshalling.IDDirect(webhook.WebhookId)
		}
	}
	require.NotZero(t, wh1ID)
	require.NotZero(t, wh2ID)

	// Create some delivery logs for wh2
	log1 := &entities.WebhookLog{
		WebhookID:    wh2ID,
		StatusCode:   utils.PtrFromValue(200),
		AttemptCount: 1,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log1)
	require.NoError(t, err)

	log2 := &entities.WebhookLog{
		WebhookID:    wh2ID,
		StatusCode:   utils.PtrFromValue(500),
		AttemptCount: 1,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log2)
	require.NoError(t, err)

	// Update config to remove wh2
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	// List webhooks again - should trigger sync and delete wh2
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)

	// Verify wh2 is deleted from DB
	_, err = suite.WebhookRepo.GetByID(ctx, wh2ID)
	require.Error(t, err)

	// Verify delivery logs for wh2 are also deleted
	logs, err := suite.WebhookLogRepo.ListByWebhook(ctx, wh2ID, pagination.Options{})
	require.NoError(t, err)
	require.Empty(t, logs.Result)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_UpdatedWebhookInConfig() {
	t := suite.T()
	ctx := context.Background()

	// Create initial config with one webhook
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Original Name"
      description: "Original description"
      url: "%s/webhook1"
      secret: "original-secret"
      ssl_verification: true
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First List call creates webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "Original Name", *resp.Webhooks[0].Name)
	require.Equal(t, "Original description", *resp.Webhooks[0].Description)
	require.True(t, strings.HasSuffix(resp.Webhooks[0].Url, "/webhook1"))
	require.True(t, resp.Webhooks[0].SslVerification)
	require.Len(t, resp.Webhooks[0].Events, 1)
	require.Equal(t, "push", resp.Webhooks[0].Events[0])

	wh1ID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Update config with all fields changed
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Updated Name"
      description: "Updated description"
      url: "%s/webhook1-updated"
      secret: "updated-secret"
      ssl_verification: false
      active: false
  on:
    push:
      - hooks: [wh1]
    refs_update:
      - hooks: [wh1]
`)

	// List webhooks again - should trigger sync and update wh1
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, "Updated Name", *resp.Webhooks[0].Name)
	require.Equal(t, "Updated description", *resp.Webhooks[0].Description)
	require.True(t, strings.HasSuffix(resp.Webhooks[0].Url, "/webhook1-updated"))
	require.False(t, resp.Webhooks[0].SslVerification)
	require.Len(t, resp.Webhooks[0].Events, 2)
	require.Contains(t, resp.Webhooks[0].Events, "push")
	require.Contains(t, resp.Webhooks[0].Events, "refs_update")
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, resp.Webhooks[0].Status)

	// Verify in DB
	dbWebhook, err := suite.WebhookRepo.GetByID(ctx, wh1ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Name", *dbWebhook.Name)
	require.Equal(t, "Updated description", *dbWebhook.Description)
	require.True(t, strings.HasSuffix(dbWebhook.PayloadURL, "/webhook1-updated"))
	require.Equal(t, "updated-secret", *dbWebhook.Secret)
	require.False(t, dbWebhook.SSLVerification)
	require.Equal(t, entities.WebhookStatuses.Inactive, dbWebhook.Status)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_AddedWebhookInConfig() {
	t := suite.T()
	ctx := context.Background()

	// Create initial config with one webhook
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First List call creates webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)

	// Add a second webhook to config
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
    - slug: wh2
      name: "Webhook2"
      description: "New webhook"
      url: "%s/webhook2"
      secret: "wh2-secret"
      ssl_verification: false
      active: false
  on:
    push:
      - hooks: [wh1]
    refs_update:
      - hooks: [wh2]
    repository.push:
      - hooks: [wh2]
`)

	// List webhooks again - should trigger sync and create wh2
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 2)

	// Find wh2 in response
	var wh2 *pb.Webhook
	for _, webhook := range resp.Webhooks {
		if webhook.Slug == "wh2" {
			wh2 = webhook
			break
		}
	}
	require.NotNil(t, wh2)
	require.Equal(t, "Webhook2", *wh2.Name)
	require.Equal(t, "New webhook", *wh2.Description)
	require.True(t, strings.HasSuffix(wh2.Url, "/webhook2"))
	require.False(t, wh2.SslVerification)
	require.Len(t, wh2.Events, 2)
	require.Contains(t, wh2.Events, "refs_update")
	require.Contains(t, wh2.Events, "repository.push")
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, wh2.Status)

	// Verify in DB
	wh2ID, err := grpc_marshalling.IDDirect(wh2.WebhookId)
	require.NoError(t, err)
	dbWebhook, err := suite.WebhookRepo.GetByID(ctx, wh2ID)
	require.NoError(t, err)
	require.Equal(t, "wh2", dbWebhook.Slug)
	require.Equal(t, "Webhook2", *dbWebhook.Name)
	require.Equal(t, entities.WebhookStatuses.Inactive, dbWebhook.Status)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_MultipleChanges() {
	t := suite.T()
	ctx := context.Background()

	// Create initial config with three webhooks
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
    - slug: wh2
      name: "Webhook2"
      url: "%s/webhook2"
      active: true
    - slug: wh3
      name: "Webhook3"
      url: "%s/webhook3"
      active: true
  on:
    push:
      - hooks: [wh1, wh2, wh3]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First List call creates webhooks in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)

	// Get wh3 ID for later verification
	var wh3ID uint64
	for _, webhook := range resp.Webhooks {
		if webhook.Slug == "wh3" {
			wh3ID, _ = grpc_marshalling.IDDirect(webhook.WebhookId)
			break
		}
	}
	require.NotZero(t, wh3ID)

	// Update config: delete wh3, update wh1, add wh4
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1 Updated"
      url: "%s/webhook1-new"
      active: true
    - slug: wh2
      name: "Webhook2"
      url: "%s/webhook2"
      active: true
    - slug: wh4
      name: "Webhook4"
      url: "%s/webhook4"
      active: false
  on:
    push:
      - hooks: [wh1, wh2]
    refs_update:
      - hooks: [wh1]
    repository.push:
      - hooks: [wh4]
`)

	// List webhooks again - should trigger sync
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3) // wh1, wh2, wh4

	// Verify wh1 was updated
	var wh1 *pb.Webhook
	for _, webhook := range resp.Webhooks {
		if webhook.Slug == "wh1" {
			wh1 = webhook
			break
		}
	}
	require.NotNil(t, wh1)
	require.Equal(t, "Webhook1 Updated", *wh1.Name)
	require.True(t, strings.HasSuffix(wh1.Url, "/webhook1-new"))
	require.Len(t, wh1.Events, 2)

	// Verify wh4 was added
	var wh4 *pb.Webhook
	for _, webhook := range resp.Webhooks {
		if webhook.Slug == "wh4" {
			wh4 = webhook
			break
		}
	}
	require.NotNil(t, wh4)
	require.Equal(t, "Webhook4", *wh4.Name)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, wh4.Status)

	// Verify wh3 was deleted
	_, err = suite.WebhookRepo.GetByID(ctx, wh3ID)
	require.Error(t, err)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_PreservesAutoDisabledStatus() {
	t := suite.T()
	ctx := context.Background()

	// Create initial config with webhook set to active
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// First List call creates webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, resp.Webhooks[0].Status)

	wh1ID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Manually set webhook to temporary_disabled (simulating auto-disable due to failures)
	timeNow := time.Now().UTC()
	nextAt := timeNow.Add(5 * time.Minute)
	err = suite.WebhookRepo.Update(wh1ID).
		SetStatus(entities.WebhookStatuses.TemporaryDisabled).
		SetTemporaryDisableCount(1).
		SetTemporaryDisabledAt(&timeNow).
		SetNextAvailableAt(&nextAt).
		Commit(ctx)
	require.NoError(t, err)

	// Update config - change URL but keep active: true
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1-updated"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	// List webhooks again - should update URL but preserve temporary_disabled status
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.True(t, strings.HasSuffix(resp.Webhooks[0].Url, "/webhook1-updated"))
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_TEMPORARY_DISABLED, resp.Webhooks[0].Status)

	// Verify in DB that status is still temporary_disabled
	dbWebhook, err := suite.WebhookRepo.GetByID(ctx, wh1ID)
	require.NoError(t, err)
	require.Equal(t, entities.WebhookStatuses.TemporaryDisabled, dbWebhook.Status)
	require.True(t, strings.HasSuffix(dbWebhook.PayloadURL, "/webhook1-updated"))

	// Now test with disabled status (manually disabled by user)
	err = suite.WebhookRepo.Update(wh1ID).SetStatus(entities.WebhookStatuses.Disabled).Commit(ctx)
	require.NoError(t, err)

	// Update config again - change description
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      description: "Updated description"
      url: "%s/webhook1-updated"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	// List webhooks - should update description but preserve disabled status
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "Updated description", *resp.Webhooks[0].Description)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_DISABLED, resp.Webhooks[0].Status)

	// Verify in DB
	dbWebhook, err = suite.WebhookRepo.GetByID(ctx, wh1ID)
	require.NoError(t, err)
	require.Equal(t, entities.WebhookStatuses.Disabled, dbWebhook.Status)
	require.Equal(t, "Updated description", *dbWebhook.Description)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_ReAddWebhookAfterDeletion() {
	t := suite.T()
	ctx := context.Background()

	// Step 1: Add webhook to config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Step 2: Sync to create webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)

	firstWebhookID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Step 3: Create delivery logs for the webhook
	log1 := &entities.WebhookLog{
		WebhookID:    firstWebhookID,
		StatusCode:   utils.PtrFromValue(200),
		AttemptCount: 1,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log1)
	require.NoError(t, err)

	log2 := &entities.WebhookLog{
		WebhookID:    firstWebhookID,
		StatusCode:   utils.PtrFromValue(500),
		AttemptCount: 2,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log2)
	require.NoError(t, err)

	// Verify logs exist (may be more than 2 if webhook dispatcher triggered on push)
	logs, err := suite.WebhookLogRepo.ListByWebhook(ctx, firstWebhookID, pagination.Options{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(logs.Result), 2, "Should have at least 2 delivery logs")

	// Step 4: Remove webhook from config
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks: []
`)

	// Sync to soft-delete webhook and its deliveries
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 0)

	// Verify webhook is soft-deleted
	_, err = suite.WebhookRepo.GetByID(ctx, firstWebhookID)
	require.Error(t, err)

	// Verify delivery logs are soft-deleted
	logs, err = suite.WebhookLogRepo.ListByWebhook(ctx, firstWebhookID, pagination.Options{})
	require.NoError(t, err)
	require.Empty(t, logs.Result)

	// Step 5: Re-add the same webhook to config
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1 Restored"
      url: "%s/webhook1-new"
      active: true
  on:
    push:
      - hooks: [wh1]
    refs_update:
      - hooks: [wh1]
`)

	// Step 6: Sync should create a NEW webhook (not resurrect the old one)
	// This tests that unique constraints work with soft-deleted records
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, "Webhook1 Restored", *resp.Webhooks[0].Name)
	require.True(t, strings.HasSuffix(resp.Webhooks[0].Url, "/webhook1-new"))
	require.Len(t, resp.Webhooks[0].Events, 2)

	secondWebhookID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Verify it's a NEW webhook (different ID)
	require.NotEqual(t, firstWebhookID, secondWebhookID, "Re-added webhook should have a new ID, not resurrect the old one")

	// Verify new webhook exists in DB
	newWebhook, err := suite.WebhookRepo.GetByID(ctx, secondWebhookID)
	require.NoError(t, err)
	require.Equal(t, "wh1", newWebhook.Slug)
	require.Equal(t, "Webhook1 Restored", *newWebhook.Name)

	// Step 7: Verify delivery logs can be created for the re-added webhook
	// Note: webhook dispatcher may have already created logs from the commit
	initialLogs, err := suite.WebhookLogRepo.ListByWebhook(ctx, secondWebhookID, pagination.Options{})
	require.NoError(t, err)
	initialLogCount := len(initialLogs.Result)

	// Create a new delivery log for the re-added webhook
	log3 := &entities.WebhookLog{
		WebhookID:    secondWebhookID,
		StatusCode:   utils.PtrFromValue(201),
		AttemptCount: 1,
	}
	_, err = suite.WebhookLogRepo.Create(ctx, log3)
	require.NoError(t, err)

	// Verify new log was created successfully (should have one more than before)
	logs, err = suite.WebhookLogRepo.ListByWebhook(ctx, secondWebhookID, pagination.Options{})
	require.NoError(t, err)
	require.Equal(t, initialLogCount+1, len(logs.Result), "Should have one more delivery log after creating log3")

	// Verify one of the logs is our created one with status 201
	found201 := false
	for _, log := range logs.Result {
		if log.StatusCode != nil && *log.StatusCode == 201 {
			found201 = true
			break
		}
	}
	require.True(t, found201, "Should find the manually created log with status code 201")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_InactiveToActive() {
	t := suite.T()
	ctx := context.Background()

	// Step 1: Create webhook with active: false (inactive status)
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: false
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Step 2: List webhooks - should create inactive webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, resp.Webhooks[0].Status)

	webhookID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	// Verify in DB that webhook is inactive
	dbWebhook, err := suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, "wh1", dbWebhook.Slug)
	require.Equal(t, entities.WebhookStatuses.Inactive, dbWebhook.Status)

	// Step 3: Update config to set active: true
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	// Step 4: List webhooks - should sync and update status to active
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, resp.Webhooks[0].Status)

	// Verify in DB that webhook is now active
	dbWebhook, err = suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, "wh1", dbWebhook.Slug)
	require.Equal(t, entities.WebhookStatuses.Active, dbWebhook.Status)

	// Step 5: Change back to inactive
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: false
  on:
    push:
      - hooks: [wh1]
`)

	// Step 6: List webhooks - should sync and update status back to inactive
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_INACTIVE, resp.Webhooks[0].Status)

	// Verify in DB that webhook is inactive again
	dbWebhook, err = suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, "wh1", dbWebhook.Slug)
	require.Equal(t, entities.WebhookStatuses.Inactive, dbWebhook.Status)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_UpdatesTemporaryDisabledFields() {
	t := suite.T()
	ctx := context.Background()

	// Step 1: Add webhook to config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Sync to create webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, int32(0), resp.Webhooks[0].TemporaryDisableCount)
	require.Nil(t, resp.Webhooks[0].TemporaryDisabledAt)
	require.Nil(t, resp.Webhooks[0].NextAvailableAt)

	// Manually set webhook to temporary_disabled (simulating auto-disable due to failures)
	wh1ID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	timeNow := time.Now().UTC()
	nextAt := timeNow.Add(5 * time.Minute)
	err = suite.WebhookRepo.Update(wh1ID).
		SetStatus(entities.WebhookStatuses.TemporaryDisabled).
		SetTemporaryDisableCount(1).
		SetTemporaryDisabledAt(&timeNow).
		SetNextAvailableAt(&nextAt).
		Commit(ctx)
	require.NoError(t, err)

	// Check temporary_disable_count, temporary_disabled_at, next_available_at are set
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, int32(1), resp.Webhooks[0].TemporaryDisableCount)
	require.NotNil(t, resp.Webhooks[0].TemporaryDisabledAt)
	require.NotNil(t, resp.Webhooks[0].NextAvailableAt)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_EffectiveAvailble_For_TemporaryDisabled() {
	t := suite.T()
	ctx := context.Background()

	// Step 1: Add webhook to config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Sync to create webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, int32(0), resp.Webhooks[0].TemporaryDisableCount)
	require.Nil(t, resp.Webhooks[0].TemporaryDisabledAt)
	require.Nil(t, resp.Webhooks[0].NextAvailableAt)

	// Manually set webhook to temporary_disabled (simulating auto-disable due to failures)
	wh1ID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	timeNow := time.Now().UTC()
	disabledAt := timeNow.Add(-10 * time.Minute)
	nextAt := disabledAt.Add(5 * time.Minute)

	err = suite.WebhookRepo.Update(wh1ID).
		SetStatus(entities.WebhookStatuses.TemporaryDisabled).
		SetTemporaryDisableCount(1).
		SetTemporaryDisabledAt(&disabledAt).
		SetNextAvailableAt(&nextAt).
		Commit(ctx)
	require.NoError(t, err)

	// Check temporary_disable_count, temporary_disabled_at, next_available_at are set
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, resp.Webhooks[0].Status)
	require.Equal(t, int32(1), resp.Webhooks[0].TemporaryDisableCount)
	require.NotNil(t, resp.Webhooks[0].TemporaryDisabledAt)
	require.NotNil(t, resp.Webhooks[0].NextAvailableAt)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Sync_Temporary_Enabled_On_Ping() {
	t := suite.T()
	ctx := context.Background()

	// Create mock webhook server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Step 1: Add webhook to config
	wt := suite.prepareWebhooksTestWithConfig(strings.ReplaceAll(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`, "%s", server.URL))

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Sync to create webhook in DB
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)

	// Manually set webhook to temporary_disabled (simulating auto-disable due to failures)
	wh1ID, err := grpc_marshalling.IDDirect(resp.Webhooks[0].WebhookId)
	require.NoError(t, err)

	timeNow := time.Now().UTC()
	disabledAt := timeNow
	nextAt := disabledAt.Add(20 * time.Minute)

	err = suite.WebhookRepo.Update(wh1ID).
		SetStatus(entities.WebhookStatuses.TemporaryDisabled).
		SetTemporaryDisableCount(1).
		SetTemporaryDisabledAt(&disabledAt).
		SetNextAvailableAt(&nextAt).
		Commit(ctx)
	require.NoError(t, err)

	// Check temporary disabled
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_TEMPORARY_DISABLED, resp.Webhooks[0].Status)

	_, err = client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Check temporary_disable_count, temporary_disabled_at, next_available_at are set
	resp, err = client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Equal(t, pb.WebhookStatus_WEBHOOK_STATUS_ACTIVE, resp.Webhooks[0].Status)
	require.Equal(t, int32(0), resp.Webhooks[0].TemporaryDisableCount)
	require.Nil(t, resp.Webhooks[0].TemporaryDisabledAt)
	require.Nil(t, resp.Webhooks[0].NextAvailableAt)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_SignKeyGeneration() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook with config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-signkey
      name: "Webhook with SignKey"
      url: "%s/webhook"
      active: true
    - slug: wh-signkey2
      url: "%s/webhook2"
      active: true
  on:
    push:
      - hooks: [wh-signkey, wh-signkey2]
`)

	// Get webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 2)

	sort.Slice(dbWebhooks.Result, func(i, j int) bool {
		return dbWebhooks.Result[i].Slug < dbWebhooks.Result[j].Slug
	})
	webhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-signkey", webhook.Slug)

	// Verify sign_key is generated
	require.NotEmpty(t, webhook.SignKey, "sign_key should be automatically generated")

	// Verify sign_key format (UUID without dashes - 32 hex characters)
	require.Len(t, webhook.SignKey, 32, "sign_key should be 32 characters (UUID without dashes)")
	require.Regexp(t, "^[0-9a-f]{32}$", webhook.SignKey, "sign_key should be hex string")

	// List webhooks via API and verify sign_key is exposed
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 2)
	require.NotEmpty(t, resp.Webhooks[0].SignKey, "sign_key should be exposed via API")
	require.Equal(t, webhook.SignKey, resp.Webhooks[0].SignKey, "API sign_key should match DB sign_key")
	require.Equal(t, webhook.SignKey, resp.Webhooks[1].SignKey, "API sign_key should be reused per repo")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_SignKeyPersistence() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook with config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-persist
      name: "Webhook Persistence Test"
      url: "%s/webhook"
      active: true
  on:
    push:
      - hooks: [wh-persist]
`)

	// Get initial webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	initialSignKey := dbWebhooks.Result[0].SignKey
	require.NotEmpty(t, initialSignKey, "sign_key should be generated")

	// Update webhook config (change description)
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh-persist
      name: "Webhook Persistence Test Updated"
      url: "%s/webhook"
      active: true
  on:
    push:
      - hooks: [wh-persist]
    refs_update:
      - hooks: [wh-persist]
`)

	// Get updated webhook from database
	dbWebhooks, err = suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	updatedWebhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-persist", updatedWebhook.Slug)
	require.Equal(t, "Webhook Persistence Test Updated", *updatedWebhook.Name, "name should be updated")

	// Verify sign_key is NOT regenerated on update
	require.Equal(t, initialSignKey, updatedWebhook.SignKey, "sign_key should persist across config updates")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_AuthHeader() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook with auth_header config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-auth
      name: "Webhook with Auth Header"
      url: "%s/webhook"
      auth_header: "X-Custom-Auth"
      active: true
  on:
    push:
      - hooks: [wh-auth]
`)

	// Get webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	webhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-auth", webhook.Slug)

	// Verify auth_header is saved
	require.NotNil(t, webhook.AuthHeader, "auth_header should be saved")
	require.Equal(t, "X-Custom-Auth", *webhook.AuthHeader)

	// Verify sign_key is also generated
	require.NotEmpty(t, webhook.SignKey, "sign_key should be generated")

	// List webhooks via API and verify auth_header is exposed
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh-auth", resp.Webhooks[0].Slug)
	require.NotNil(t, resp.Webhooks[0].AuthHeader, "auth_header should be exposed via API")
	require.Equal(t, "X-Custom-Auth", *resp.Webhooks[0].AuthHeader)
	require.Equal(t, webhook.SignKey, resp.Webhooks[0].SignKey, "sign_key should be exposed")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_AuthHeaderUpdate() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook without auth_header initially
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-auth-update
      name: "Webhook Auth Header Update"
      url: "%s/webhook"
      active: true
  on:
    push:
      - hooks: [wh-auth-update]
`)

	// Get initial webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	webhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-auth-update", webhook.Slug)
	require.Nil(t, webhook.AuthHeader, "auth_header should be nil initially")

	// Update config to add auth_header
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh-auth-update
      name: "Webhook Auth Header Update"
      url: "%s/webhook"
      auth_header: "X-My-Auth"
      active: true
  on:
    push:
      - hooks: [wh-auth-update]
`)

	// Get updated webhook from database
	dbWebhooks, err = suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	updatedWebhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-auth-update", updatedWebhook.Slug)
	require.NotNil(t, updatedWebhook.AuthHeader, "auth_header should be set")
	require.Equal(t, "X-My-Auth", *updatedWebhook.AuthHeader)

	// Update config to change auth_header
	suite.changeWebhooksTestConfig(wt, `
webhooks:
  hooks:
    - slug: wh-auth-update
      name: "Webhook Auth Header Update"
      url: "%s/webhook"
      auth_header: "Authorization"
      active: true
  on:
    push:
      - hooks: [wh-auth-update]
`)

	// Get webhook after second update
	dbWebhooks, err = suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	finalWebhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-auth-update", finalWebhook.Slug)
	require.NotNil(t, finalWebhook.AuthHeader, "auth_header should be updated")
	require.Equal(t, "Authorization", *finalWebhook.AuthHeader)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_SignKeyAndAuthHeaderTogether() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook with both secret and auth_header
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-both
      name: "Webhook with Secret and Auth Header"
      url: "%s/webhook"
      secret: "my-webhook-secret"
      auth_header: "X-Webhook-Token"
      active: true
  on:
    push:
      - hooks: [wh-both]
`)

	// Get webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	webhook := dbWebhooks.Result[0]
	require.Equal(t, "wh-both", webhook.Slug)

	// Verify both secret and auth_header are saved
	require.NotNil(t, webhook.Secret, "secret should be saved")
	require.Equal(t, "my-webhook-secret", *webhook.Secret)
	require.NotNil(t, webhook.AuthHeader, "auth_header should be saved")
	require.Equal(t, "X-Webhook-Token", *webhook.AuthHeader)

	// Verify sign_key is also generated (even when secret is present)
	require.NotEmpty(t, webhook.SignKey, "sign_key should be generated even with secret")

	// List webhooks via API
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "wh-both", resp.Webhooks[0].Slug)
	require.True(t, resp.Webhooks[0].HasSecret, "has_secret should be true")
	require.NotNil(t, resp.Webhooks[0].AuthHeader, "auth_header should be exposed")
	require.Equal(t, "X-Webhook-Token", *resp.Webhooks[0].AuthHeader)
	require.NotEmpty(t, resp.Webhooks[0].SignKey, "sign_key should be exposed")
}

// TestWebhooksHandler_DeliveryWithAuthHeaderAndSignKey verifies that webhook delivery
// sends the custom auth header with sign_key value on push events
func (suite *RwApiTestSuite) TestWebhooksHandler_DeliveryWithAuthHeaderAndSignKey() {
	t := suite.T()
	ctx := context.Background()

	// Track captured headers from webhook delivery
	var capturedHeaders http.Header
	var capturedBody []byte

	// Create mock webhook server that captures headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		// Read body to capture payload
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Create repo and setup webhook with auth_header but no secret
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)

	protocol := suite.HTTPSProtocol()
	cgAdmin := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cgAdmin.Must(t, "init", ".")
	cgAdmin.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))
	cgAdmin.Must(t, "branch", "-m", "master")

	// Add webhooks config with auth_header and no secret
	webhookYaml := `
webhooks:
  hooks:
    - slug: push-webhook
      name: "Push Webhook"
      url: "` + server.URL + `"
      auth_header: "X-Custom-Auth"
      active: true
  on:
    push:
      - hooks: [push-webhook]
`
	suite.makeNewFileWithContent(cgAdmin, oyaml.WebhooksPath, webhookYaml)
	suite.commitAll(cgAdmin, "add webhooks config", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Get webhook info to retrieve sign_key
	_, err := suite.OrgRepo.GetOrganization(ctx, orgSlug)
	require.NoError(t, err)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	resp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(repoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 1)
	require.Equal(t, "push-webhook", resp.Webhooks[0].Slug)
	require.NotNil(t, resp.Webhooks[0].AuthHeader)
	require.Equal(t, "X-Custom-Auth", *resp.Webhooks[0].AuthHeader)
	require.NotEmpty(t, resp.Webhooks[0].SignKey)

	signKey := resp.Webhooks[0].SignKey

	// Make a push to trigger webhook delivery
	suite.makeNewFileWithContent(cgAdmin, "test.txt", "test content")
	suite.commitAll(cgAdmin, "trigger webhook", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify webhook delivery headers
	require.NotNil(t, capturedHeaders, "webhook should have been delivered")
	require.NotEmpty(t, capturedBody, "webhook payload should not be empty")

	// Verify custom auth header is present with sign_key value
	customAuthValue := capturedHeaders.Get("X-Custom-Auth")
	require.Equal(t, signKey, customAuthValue, "X-Custom-Auth header should contain sign_key value")

	// Verify X-Src-Signature header is present and uses sign_key for signing
	signature := capturedHeaders.Get("X-Src-Signature")
	require.NotEmpty(t, signature, "X-Src-Signature header should be present")

	// Verify the signature was computed with sign_key (not secret, since no secret was provided)
	// We can verify this by computing HMAC ourselves
	expectedSignature := computeHMAC(string(capturedBody), signKey)
	require.Equal(t, expectedSignature, signature, "signature should be computed using sign_key")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_GetBySlug() {
	t := suite.T()
	ctx := context.Background()

	// Prepare webhook with both secret and auth_header
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/webhook1"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	// Get webhook from database
	dbWebhooks, err := suite.WebhookRepo.ListByEntity(ctx, wt.RepoID, entities.EntityTypes.Repository, pagination.Options{})
	require.NoError(t, err)
	require.Len(t, dbWebhooks.Result, 1)

	webhook := dbWebhooks.Result[0]
	require.Equal(t, "wh1", webhook.Slug)

	// GetBySlug
	client := pb.NewWebhookServiceClient(suite.grpcClient)
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	resp, err := client.GetBySlug(grpcCtx, &pb.GetWebhookBySlugRequest{
		Entity: &pb.GetWebhookBySlugRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Webhook)
	require.Equal(t, "wh1", resp.Webhook.Slug)

	resp2, err := client.GetBySlug(grpcCtx, &pb.GetWebhookBySlugRequest{
		Entity: &pb.GetWebhookBySlugRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh2",
	})
	require.Error(t, err)
	require.Nil(t, resp2)
}

// computeHMAC computes HMAC-SHA256 signature (helper function for test)
func computeHMAC(payload, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping() {
	t := suite.T()
	ctx := context.Background()

	// Track webhook delivery
	var receivedPing bool
	var capturedHeaders http.Header
	var capturedBody []byte

	// Create mock webhook server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPing = true
		capturedHeaders = r.Header.Clone()
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	// Prepare webhook with config
	wt := suite.prepareWebhooksTestWithConfig(strings.ReplaceAll(`
webhooks:
  hooks:
    - slug: wh-ping
      name: "Webhook Ping Test"
      url: "`+server.URL+`"
      active: true
  on:
    push:
      - hooks: [wh-ping]
`, "%s", server.URL))

	// List webhooks to ensure they're synced to DB
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, "wh-ping", listResp.Webhooks[0].Slug)

	// Call Ping - should return operation directly
	pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping",
	})
	require.NoError(t, err)
	require.NotNil(t, pingResp)
	require.NotEmpty(t, pingResp.Id, "Ping should return an operation with ID")

	// Initially, operation should not be done yet
	require.False(t, pingResp.Done, "operation should not be done initially")

	// Wait for webhook workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify ping was received
	require.True(t, receivedPing, "webhook should have received ping")
	require.NotNil(t, capturedHeaders)
	require.NotEmpty(t, capturedBody)

	// Verify payload contains ping event
	var payload map[string]any
	err = json.Unmarshal(capturedBody, &payload)
	require.NoError(t, err)

	header := payload["header"].(map[string]any)
	require.NotNil(t, header)
	require.NotNil(t, header["id"])
	eventID := header["id"].(string)
	require.NotEmpty(t, eventID)
	require.Equal(t, "webhook.ping", header["type"])
	require.Equal(t, "webhook", header["aggregate_type"])
	require.Equal(t, "wh-ping", header["aggregate_id"])
	require.NotNil(t, payload["repository"])
	require.Equal(t, "wh-ping", payload["webhook_slug"])
	require.NotNil(t, payload["pinged_at"])

	// Verify delivery log was created
	deliveriesResp, err := client.GetDeliveries(grpcCtx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping",
	})
	require.NoError(t, err)
	require.NotEmpty(t, deliveriesResp.Deliveries)

	// Find the ping delivery
	var pingDelivery *pb.WebhookDelivery
	for _, delivery := range deliveriesResp.Deliveries {
		if strings.Contains(delivery.EventId, eventID) {
			pingDelivery = delivery
			break
		}
	}
	require.NotNil(t, pingDelivery, "should have a ping delivery log")
	require.Equal(t, int32(200), pingDelivery.StatusCode)

	// Verify webhook in DB
	webhookID, err := grpc_marshalling.IDDirect(listResp.Webhooks[0].WebhookId)
	require.NoError(t, err)
	dbWebhook, err := suite.WebhookRepo.GetByID(ctx, webhookID)
	require.NoError(t, err)
	require.Equal(t, "wh-ping", dbWebhook.Slug)

	// After workflow completes, get the updated operation status
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	opResp, err := opClient.Get(grpcCtx, &pb.GetOperationRequest{
		Id: pingResp.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, opResp)
	require.True(t, opResp.Done, "operation should be done after webhook delivery")

	// Check metadata for success status
	metadata := testutils.UnmarshalGrpcMetadata[*pb.OperationMetadata](t, opResp)
	require.Equal(t, pb.OperationMetadata_SUCCESS, metadata.Status, "operation should be successful after webhook delivery succeeds")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_WebhookNotFound() {
	t := suite.T()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Try to ping non-existent webhook
	resp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "nonexistent",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "nonexistent")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_InactiveWebhook() {
	t := suite.T()

	// Track webhook delivery
	var receivedPing bool
	var capturedHeaders http.Header
	var capturedBody []byte

	// Create mock webhook server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPing = true
		capturedHeaders = r.Header.Clone()
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	// Prepare webhook with active: false
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-inactive
      name: "Inactive Webhook"
      url: "` + server.URL + `"
      active: false
  on:
    push:
      - hooks: [wh-inactive]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Try to ping inactive webhook - should still work
	resp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-inactive",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Id, "Ping should return an operation with ID")

	// Wait for webhook workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify ping was received
	require.True(t, receivedPing, "webhook should have received ping")
	require.NotNil(t, capturedHeaders)
	require.NotEmpty(t, capturedBody)

	// Verify operation status is SUCCESS
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	opResp, err := opClient.Get(grpcCtx, &pb.GetOperationRequest{
		Id: resp.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, opResp)
	require.True(t, opResp.Done, "operation should be done after webhook delivery")

	// Check metadata for success status
	metadata := testutils.UnmarshalGrpcMetadata[*pb.OperationMetadata](t, opResp)
	require.Equal(t, pb.OperationMetadata_SUCCESS, metadata.Status, "operation should be successful after webhook delivery succeeds")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_UnauthorizedAccess() {
	t := suite.T()

	// Create repo with Kopatych as owner
	repoID, orgSlug, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	_, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	_, err = suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	// Try to ping with different user (Krosh) who doesn't have permissions
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	resp, err := client.Ping(ctx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(repoID),
		},
		WebhookSlug: "wh1",
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_WithAuthHeader() {
	t := suite.T()

	// Track webhook delivery
	var receivedPing bool
	var capturedHeaders http.Header
	var capturedBody []byte

	// Create mock webhook server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPing = true
		capturedHeaders = r.Header.Clone()
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer server.Close()

	// Prepare webhook with auth_header
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-ping-auth
      name: "Webhook Ping with Auth"
      url: "` + server.URL + `"
      auth_header: "X-Custom-Auth"
      active: true
  on:
    push:
      - hooks: [wh-ping-auth]
`)

	// List webhooks to get sign_key
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, "wh-ping-auth", listResp.Webhooks[0].Slug)
	require.NotNil(t, listResp.Webhooks[0].AuthHeader)
	require.Equal(t, "X-Custom-Auth", *listResp.Webhooks[0].AuthHeader)
	signKey := listResp.Webhooks[0].SignKey

	// Call Ping
	pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping-auth",
	})
	require.NoError(t, err)
	require.NotNil(t, pingResp)
	require.NotEmpty(t, pingResp.Id, "Ping should return an operation with ID")

	// Wait for webhook workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify ping was received
	require.True(t, receivedPing, "webhook should have received ping")
	require.NotNil(t, capturedHeaders)
	require.NotEmpty(t, capturedBody)

	// Verify custom auth header is present with sign_key value
	customAuthValue := capturedHeaders.Get("X-Custom-Auth")
	require.Equal(t, signKey, customAuthValue, "X-Custom-Auth header should contain sign_key value")

	// Verify X-Src-Signature header is present
	signature := capturedHeaders.Get("X-Src-Signature")
	require.NotEmpty(t, signature, "X-Src-Signature header should be present")

	// Verify the signature was computed with sign_key
	expectedSignature := computeHMAC(string(capturedBody), signKey)
	require.Equal(t, expectedSignature, signature, "signature should be computed using sign_key")

	// Verify operation status is SUCCESS
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	opResp, err := opClient.Get(grpcCtx, &pb.GetOperationRequest{
		Id: pingResp.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, opResp)
	require.True(t, opResp.Done, "operation should be done after webhook delivery")

	// Check metadata for success status
	metadata := testutils.UnmarshalGrpcMetadata[*pb.OperationMetadata](t, opResp)
	require.Equal(t, pb.OperationMetadata_SUCCESS, metadata.Status, "operation should be successful after webhook delivery succeeds")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_FailedDelivery() {
	t := suite.T()

	isFailed := false
	// Create mock webhook server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isFailed {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}
	}))
	defer server.Close()

	// Prepare webhook with config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-ping-fail
      name: "Webhook Ping Fail Test"
      url: "` + server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-ping-fail]
`)

	// List webhooks to ensure they're synced to DB
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	listResp, err := client.List(grpcCtx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, listResp.Webhooks, 1)
	require.Equal(t, "wh-ping-fail", listResp.Webhooks[0].Slug)

	// Call Ping - should return operation ID
	isFailed = true
	pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping-fail",
	})
	require.NoError(t, err)
	require.NotNil(t, pingResp)
	require.NotEmpty(t, pingResp.Id, "Ping should return an operation with ID")

	// Wait for webhook workflow to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// After workflow completes, verify operation is done
	// Note: The workflow retries failed deliveries, so a 500 error might eventually succeed or fail
	// We just verify the operation exists and gets completed
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	opResp, err := opClient.Get(grpcCtx, &pb.GetOperationRequest{
		Id: pingResp.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, opResp)
	require.True(t, opResp.Done, "operation should be done after webhook delivery completes")

	// Check metadata - status can be either SUCCESS (if retries succeeded) or FAILED (if all retries failed)
	metadata := testutils.UnmarshalGrpcMetadata[*pb.OperationMetadata](t, opResp)
	require.Contains(t, []pb.OperationMetadata_Status{pb.OperationMetadata_SUCCESS, pb.OperationMetadata_FAILED},
		metadata.Status, "operation should have a final status (success or failed)")

	// Verify delivery log shows failure
	deliveriesResp, err := client.GetDeliveries(grpcCtx, &pb.GetWebhookDeliveriesRequest{
		Entity: &pb.GetWebhookDeliveriesRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping-fail",
	})
	require.NoError(t, err)
	require.NotEmpty(t, deliveriesResp.Deliveries)

	// Find the most recent delivery
	var failedDelivery *pb.WebhookDelivery
	for _, delivery := range deliveriesResp.Deliveries {
		if failedDelivery == nil || delivery.CreatedAt.Seconds > failedDelivery.CreatedAt.Seconds {
			failedDelivery = delivery
		}
	}
	require.NotNil(t, failedDelivery, "should have a failed delivery log")
	require.Equal(t, int32(500), failedDelivery.StatusCode)
}
