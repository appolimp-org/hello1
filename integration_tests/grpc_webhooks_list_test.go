package integrationtests

import (
	"common/utils"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"

	"github.com/stretchr/testify/require"

	"private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_FirstPage() {
	t := suite.T()

	// Create 5 webhooks to test pagination
	configYaml := `
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
    - slug: wh4
      name: "Webhook4"
      url: "%s/webhook4"
      active: true
    - slug: wh5
      name: "Webhook5"
      url: "%s/webhook5"
      active: true
  on:
    push:
      - hooks: [wh1, wh2, wh3, wh4, wh5]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request first page with page_size = 2
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		PageSize: utils.PtrFromValue(uint64(2)),
		SortBy:   []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_ASC}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 2)
	require.NotEmpty(t, resp.NextPageToken)
	require.Empty(t, resp.PrevPageToken)

	// Verify webhooks are returned
	require.Equal(t, "wh1", resp.Webhooks[0].Slug)
	require.Equal(t, "wh2", resp.Webhooks[1].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_SecondPage() {
	t := suite.T()

	configYaml := `
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
    - slug: wh4
      name: "Webhook4"
      url: "%s/webhook4"
      active: true
    - slug: wh5
      name: "Webhook5"
      url: "%s/webhook5"
      active: true
  on:
    push:
      - hooks: [wh1, wh2, wh3, wh4, wh5]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request first page
	resp1, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		PageSize: utils.PtrFromValue(uint64(2)),
		SortBy:   []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_ASC}},
	})
	require.NoError(t, err)
	require.Len(t, resp1.Webhooks, 2)
	require.NotEmpty(t, resp1.NextPageToken)

	// Verify first page
	require.Equal(t, "wh1", resp1.Webhooks[0].Slug)
	require.Equal(t, "wh2", resp1.Webhooks[1].Slug)

	// Request second page using next_page_token
	resp2, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		PageSize:  utils.PtrFromValue(uint64(2)),
		SortBy:    []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_ASC}},
		PageToken: utils.PtrFromValue(resp1.NextPageToken),
	})
	require.NoError(t, err)
	require.Len(t, resp2.Webhooks, 2)
	require.NotEmpty(t, resp2.NextPageToken)
	require.NotEmpty(t, resp2.PrevPageToken)

	// Verify second page
	require.Equal(t, "wh3", resp2.Webhooks[0].Slug)
	require.Equal(t, "wh4", resp2.Webhooks[1].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_LastPage() {
	t := suite.T()

	configYaml := `
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
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request first page
	resp1, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		PageSize: utils.PtrFromValue(uint64(2)),
		SortBy:   []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_ASC}},
	})
	require.NoError(t, err)
	require.Len(t, resp1.Webhooks, 2)
	require.NotEmpty(t, resp1.NextPageToken)

	// Request last page using next_page_token
	resp2, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		PageSize:  utils.PtrFromValue(uint64(2)),
		SortBy:    []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_ASC}},
		PageToken: utils.PtrFromValue(resp1.NextPageToken),
	})
	require.NoError(t, err)
	require.Len(t, resp2.Webhooks, 1) // Only 1 webhook left
	require.Empty(t, resp2.NextPageToken)
	require.NotEmpty(t, resp2.PrevPageToken)
	require.Equal(t, "wh3", resp2.Webhooks[0].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_AllPages() {
	t := suite.T()

	configYaml := `
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
    - slug: wh4
      name: "Webhook4"
      url: "%s/webhook4"
      active: true
    - slug: wh5
      name: "Webhook5"
      url: "%s/webhook5"
      active: true
  on:
    push:
      - hooks: [wh1, wh2, wh3, wh4, wh5]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	slugs := []string{}
	var pageToken *string
	pageCount := 0

	// Iterate through all pages
	for {
		req := &pb.ListWebhooksRequest{
			Entity: &pb.ListWebhooksRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt.RepoID),
			},
			PageSize: utils.PtrFromValue(uint64(2)),
			SortBy:   []*pagination.SortOption{{Column: "slug", Direction: pagination.SortOption_DESC}},
		}
		if pageToken != nil {
			req.PageToken = pageToken
		}

		resp, err := client.List(ctx, req)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Webhooks)

		// Collect webhooks from this page
		for _, wh := range resp.Webhooks {
			slugs = append(slugs, wh.Slug)
		}

		pageCount++
		if resp.NextPageToken == "" {
			break
		}
		pageToken = &resp.NextPageToken
	}

	// Verify we got all 5 webhooks
	require.Len(t, slugs, 5)
	require.Equal(t, "wh5", slugs[0])
	require.Equal(t, "wh4", slugs[1])
	require.Equal(t, "wh3", slugs[2])
	require.Equal(t, "wh2", slugs[3])
	require.Equal(t, "wh1", slugs[4])
	require.Equal(t, 3, pageCount) // Should have 3 pages (2+2+1)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_SortBySlug() {
	t := suite.T()

	configYaml := `
webhooks:
  hooks:
    - slug: charlie
      name: "Charlie"
      url: "%s/charlie"
      active: true
    - slug: alice
      name: "Alice"
      url: "%s/alice"
      active: true
    - slug: bob
      name: "Bob"
      url: "%s/bob"
      active: true
  on:
    push:
      - hooks: [charlie, alice, bob]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request sorted by slug ascending
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*pagination.SortOption{
			{
				Column:    "slug",
				Direction: pagination.SortOption_ASC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)
	require.Equal(t, "alice", resp.Webhooks[0].Slug)
	require.Equal(t, "bob", resp.Webhooks[1].Slug)
	require.Equal(t, "charlie", resp.Webhooks[2].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_SortBySlugDescending() {
	t := suite.T()

	configYaml := `
webhooks:
  hooks:
    - slug: charlie
      name: "Charlie"
      url: "%s/charlie"
      active: true
    - slug: alice
      name: "Alice"
      url: "%s/alice"
      active: true
    - slug: bob
      name: "Bob"
      url: "%s/bob"
      active: true
  on:
    push:
      - hooks: [charlie, alice, bob]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request sorted by slug descending
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*pagination.SortOption{
			{
				Column:    "slug",
				Direction: pagination.SortOption_DESC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)
	require.Equal(t, "charlie", resp.Webhooks[0].Slug)
	require.Equal(t, "bob", resp.Webhooks[1].Slug)
	require.Equal(t, "alice", resp.Webhooks[2].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_SortByPayloadURL() {
	t := suite.T()

	configYaml := `
webhooks:
  hooks:
    - slug: wh1
      name: "Webhook1"
      url: "%s/zzz"
      active: true
    - slug: wh2
      name: "Webhook2"
      url: "%s/aaa"
      active: true
    - slug: wh3
      name: "Webhook3"
      url: "%s/mmm"
      active: true
  on:
    push:
      - hooks: [wh1, wh2, wh3]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request sorted by payload_url ascending
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*pagination.SortOption{
			{
				Column:    "payload_url",
				Direction: pagination.SortOption_ASC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)
	require.Equal(t, "wh2", resp.Webhooks[0].Slug)
	require.Equal(t, "wh3", resp.Webhooks[1].Slug)
	require.Equal(t, "wh1", resp.Webhooks[2].Slug)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_EmptyResult() {
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
		PageSize: utils.PtrFromValue(uint64(10)),
	})
	require.NoError(t, err)
	require.Empty(t, resp.Webhooks)
	require.Empty(t, resp.NextPageToken)
	require.Empty(t, resp.PrevPageToken)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_WithoutPageSize() {
	t := suite.T()

	configYaml := `
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
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request without page_size (should return all results)
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 3)
	require.Empty(t, resp.NextPageToken)
	require.Empty(t, resp.PrevPageToken)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_List_Pagination_SortByMultipleColumns() {
	t := suite.T()

	// Create webhooks with different creation times by using the commit sequence
	configYaml1 := `
webhooks:
  hooks:
    - slug: aaa-webhook
      name: "AAA Webhook"
      url: "%s/aaa"
      active: true
  on:
    push:
      - hooks: [aaa-webhook]
`
	wt := suite.prepareWebhooksTestWithConfig(configYaml1)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Add more webhooks in subsequent commits
	configYaml2 := `
webhooks:
  hooks:
    - slug: aaa-webhook
      name: "AAA Webhook"
      url: "%s/aaa"
      active: true
    - slug: bbb-webhook
      name: "BBB Webhook"
      url: "%s/bbb"
      active: true
  on:
    push:
      - hooks: [aaa-webhook, bbb-webhook]
`
	suite.changeWebhooksTestConfig(wt, configYaml2)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Request sorted by created_at descending, then slug ascending
	resp, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*pagination.SortOption{
			{
				Column:    "created_at",
				Direction: pagination.SortOption_DESC,
			},
			{
				Column:    "slug",
				Direction: pagination.SortOption_ASC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Webhooks, 2)

	// bbb-webhook was created later (in the second commit), so with created_at DESC it should come first
	// aaa-webhook was created earlier (in the first commit), so it should come second
	require.Equal(t, "bbb-webhook", resp.Webhooks[0].Slug)
	require.Equal(t, "aaa-webhook", resp.Webhooks[1].Slug)

	// Request sorted by created_at descending, then slug ascending
	resp2, err := client.List(ctx, &pb.ListWebhooksRequest{
		Entity: &pb.ListWebhooksRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		SortBy: []*pagination.SortOption{
			{
				Column:    "slug",
				Direction: pagination.SortOption_ASC,
			},
			{
				Column:    "created_at",
				Direction: pagination.SortOption_DESC,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp2.Webhooks, 2)

	require.Equal(t, "aaa-webhook", resp2.Webhooks[0].Slug)
	require.Equal(t, "bbb-webhook", resp2.Webhooks[1].Slug)
}
