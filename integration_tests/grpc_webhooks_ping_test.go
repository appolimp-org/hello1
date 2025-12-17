package integrationtests

import (
	"encoding/json"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/webhook"
	"gitcore/internal/testutils"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stretchr/testify/require"

	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	operation "bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
)

// pingTestServer wraps an HTTP test server for webhook ping testing
type pingTestServer struct {
	Server        *httptest.Server
	PingCount     int32
	ReceivedPings []map[string]any
	mu            sync.Mutex
	responseBody  string
	storePayloads bool
}

// GetPingCount returns the current ping count in a thread-safe manner
func (s *pingTestServer) GetPingCount() int32 {
	return atomic.LoadInt32(&s.PingCount)
}

// GetReceivedPings returns a copy of received pings in a thread-safe manner
func (s *pingTestServer) GetReceivedPings() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]map[string]any, len(s.ReceivedPings))
	copy(result, s.ReceivedPings)
	return result
}

// Close closes the underlying test server
func (s *pingTestServer) Close() {
	s.Server.Close()
}

func (suite *RwApiTestSuite) createPingTestServer(responseBody string, storePayloads bool) *pingTestServer {
	pts := &pingTestServer{
		responseBody:  responseBody,
		storePayloads: storePayloads,
		ReceivedPings: make([]map[string]any, 0),
	}

	pts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)

		var payload map[string]any
		json.Unmarshal(body, &payload)

		// Check if this is a ping event by looking at header.type
		// The protobuf serialization creates: {"header": {"type": "webhook.ping"}, ...}
		isPing := false
		if header, ok := payload["header"].(map[string]any); ok {
			if eventType, ok := header["type"].(string); ok && eventType == "webhook.ping" {
				isPing = true
			}
		}

		// Only count ping events, ignore push events from config commits
		if isPing {
			atomic.AddInt32(&pts.PingCount, 1)
			if pts.storePayloads {
				pts.mu.Lock()
				pts.ReceivedPings = append(pts.ReceivedPings, payload)
				pts.mu.Unlock()
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(pts.responseBody))
	}))

	return pts
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_RateLimit() {
	t := suite.T()

	// Create mock webhook server
	server := suite.createPingTestServer("pong", true)
	defer server.Close()

	// Prepare webhook with config
	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-ping-limit
      name: "Webhook Ping Rate Limit Test"
      url: "` + server.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-ping-limit]
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
	require.Equal(t, "wh-ping-limit", listResp.Webhooks[0].Slug)

	// Verify the configured limit is 10
	require.Equal(t, 10, webhook.PingWebhookRateLimit, "ping rate limit should be 10 per minute")

	// Send pings up to the rate limit (10 per minute)
	// We'll send 10 requests which should all succeed
	successfulPings := 0
	for range 10 {
		pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
			Entity: &pb.PingWebhookRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt.RepoID),
			},
			WebhookSlug: "wh-ping-limit",
		})
		require.NoError(t, err)
		require.NotNil(t, pingResp)
		successfulPings++
	}

	// Wait for webhook workflows to complete
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Verify all 10 pings were received
	require.Equal(t, int32(10), server.GetPingCount(), "should receive exactly 10 pings")
	require.Equal(t, 10, successfulPings, "all 10 ping requests should succeed")

	// Try to send 11th ping - should fail due to rate limit
	_, err = client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping-limit",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
	require.Equal(t, int32(10), server.GetPingCount(), "should still have exactly 10 pings, 11th blocked by rate limit")

	// Verify all received pings have correct event type and webhook slug
	receivedPings := server.GetReceivedPings()
	for i, ping := range receivedPings {
		// Check header.type for event type (protobuf serialization structure)
		header, ok := ping["header"].(map[string]any)
		require.True(t, ok, "ping %d should have a header", i+1)
		require.Equal(t, "webhook.ping", header["type"], "ping %d should have event type 'webhook.ping'", i+1)
		require.Equal(t, "wh-ping-limit", ping["webhook_slug"], "ping %d should have correct webhook slug", i+1)
	}
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_RateLimit_MultipleRepositories() {
	t := suite.T()

	// Server for repo 1
	server1 := suite.createPingTestServer("pong1", false)
	defer server1.Close()

	// Server for repo 2
	server2 := suite.createPingTestServer("pong2", false)
	defer server2.Close()

	// Prepare first webhook
	wt1 := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-repo1
      name: "Webhook Repo 1"
      url: "` + server1.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-repo1]
`)

	// Prepare second webhook
	wt2 := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-repo2
      name: "Webhook Repo 2"
      url: "` + server2.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-repo2]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Exhaust rate limit for repo 1 (10 pings)
	for range 10 {
		_, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
			Entity: &pb.PingWebhookRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt1.RepoID),
			},
			WebhookSlug: "wh-repo1",
		})
		require.NoError(t, err)
	}

	// Verify repo 1 received 10 pings
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)
	require.Equal(t, int32(10), server1.GetPingCount(), "repo 1 should receive 10 pings")

	// Try 11th ping on repo 1 - should fail
	_, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt1.RepoID),
		},
		WebhookSlug: "wh-repo1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")

	// Repo 2 should still be able to send pings (independent rate limit)
	for range 10 {
		_, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
			Entity: &pb.PingWebhookRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt2.RepoID),
			},
			WebhookSlug: "wh-repo2",
		})
		require.NoError(t, err)
	}

	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)
	require.Equal(t, int32(10), server2.GetPingCount(), "repo 2 should receive 10 pings independently")
	require.Equal(t, int32(10), server1.GetPingCount(), "repo 1 should still have 10 pings")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_RateLimit_BelowLimit() {
	t := suite.T()

	server := suite.createPingTestServer("pong", false)
	defer server.Close()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-below-limit
      name: "Webhook Below Limit Test"
      url: "` + server.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-below-limit]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	// Send only 5 pings (well below the 10/minute limit)
	for range 5 {
		pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
			Entity: &pb.PingWebhookRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt.RepoID),
			},
			WebhookSlug: "wh-below-limit",
		})
		require.NoError(t, err)
		require.NotNil(t, pingResp)
	}

	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// All 5 pings should be received
	require.Equal(t, int32(5), server.GetPingCount(), "should receive all 5 pings")

	// Should still be able to send more pings
	for range 3 {
		_, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
			Entity: &pb.PingWebhookRequest_RepoId{
				RepoId: grpc_marshalling.IDInverse(wt.RepoID),
			},
			WebhookSlug: "wh-below-limit",
		})
		require.NoError(t, err)
	}

	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	// Should now have 8 total pings
	require.Equal(t, int32(8), server.GetPingCount(), "should receive all 8 pings total")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Ping_OperationError() {
	t := suite.T()

	isError := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isError {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}
	}))
	defer server.Close()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh-ping-error
      name: "Webhook ping error"
      url: "` + server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh-ping-error]
`)

	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewWebhookServiceClient(suite.grpcClient)

	isError = true
	pingResp, err := client.Ping(grpcCtx, &pb.PingWebhookRequest{
		Entity: &pb.PingWebhookRequest_RepoId{
			RepoId: grpc_marshalling.IDInverse(wt.RepoID),
		},
		WebhookSlug: "wh-ping-error",
	})
	require.NoError(t, err)
	require.NotNil(t, pingResp)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	opResp, err := opClient.Get(grpcCtx, &pb.GetOperationRequest{
		Id: pingResp.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, opResp)

	opErr, ok := opResp.Result.(*operation.Operation_Error)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(opErr.Error.Message, "Got HTTP 500 error"))
}
