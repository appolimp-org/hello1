package integrationtests

import (
	"context"
	"fmt"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// AuthTransport is a custom RoundTripper that adds the Authorization header
type AuthTransport struct {
	Token     string
	Transport http.RoundTripper
}

func (a *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", a.Token))
	return a.Transport.RoundTrip(req)
}

func (suite *RwApiTestSuite) TestMCP() {
	t := suite.T()
	ctx := context.Background()

	authToken := suite.getFakeAuthenticator(suite.users.Admin.Identity).MarshalToStruct().Token
	authTransport := &AuthTransport{
		Token:     authToken,
		Transport: http.DefaultTransport,
	}
	httpClient := &http.Client{Transport: authTransport}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "SourceCraft MCP Client",
		Version: "1.0.0",
	}, nil)
	mcpClientTransport := mcp.NewStreamableClientTransport(
		fmt.Sprintf("http://127.0.0.1:%d/mcp", suite.cfg.PublicAPI.Port),
		&mcp.StreamableClientTransportOptions{
			HTTPClient: httpClient,
		},
	)

	t.Run("unauthenticated", func(t *testing.T) {
		_, err := mcpClient.Connect(ctx, mcp.NewStreamableClientTransport(
			fmt.Sprintf("http://127.0.0.1:%d/mcp", suite.cfg.PublicAPI.Port), &mcp.StreamableClientTransportOptions{},
		))
		require.Error(t, err, "calling \"initialize\": broken session: 401 Unauthorized")
	})

	t.Run("not existing tool", func(t *testing.T) {
		cs, err := mcpClient.Connect(ctx, mcpClientTransport)
		require.NoError(t, err)
		defer cs.Close()

		_, err = cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "CreateSomething",
		})
		require.Error(t, err, "calling \"tools/call\": invalid params: unknown tool \"CreateSomething\"")
	})

	t.Run("invalid arguments", func(t *testing.T) {
		cs, err := mcpClient.Connect(ctx, mcpClientTransport)
		require.NoError(t, err)
		defer cs.Close()

		_, err = cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "CreateIssue",
			Arguments: &pbPub.CreateIssueRequest{
				Body: &pbPub.CreateIssueBody{
					Title: "Test issue",
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "required: missing properties: [\"org_slug\" \"repo_slug\"]")
	})

	t.Run("create and get issue", func(t *testing.T) {
		cs, err := mcpClient.Connect(ctx, mcpClientTransport)
		require.NoError(t, err)
		defer cs.Close()

		createIssueRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "CreateIssue",
			Arguments: &pbPub.CreateIssueRequest{
				OrgSlug:  suite.repos.Alpha.OrgSlug,
				RepoSlug: suite.repos.Alpha.Slug,
				Body: &pbPub.CreateIssueBody{
					Title: "Test issue",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, createIssueRes.IsError)
		require.Len(t, createIssueRes.Content, 1)
		textContent, ok := createIssueRes.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		createPb := &pbPub.Issue{}
		err = protojson.Unmarshal([]byte(textContent.Text), createPb)
		require.NoError(t, err)

		getIssueRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "GetIssue",
			Arguments: &pbPub.GetIssueRequest{
				OrgSlug:   suite.repos.Alpha.OrgSlug,
				RepoSlug:  suite.repos.Alpha.Slug,
				IssueSlug: createPb.Slug,
			},
		})
		require.NoError(t, err)
		require.False(t, getIssueRes.IsError)
		require.Len(t, getIssueRes.Content, 1)
		textContent, ok = getIssueRes.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		getPb := &pbPub.Issue{}
		err = protojson.Unmarshal([]byte(textContent.Text), getPb)
		require.NoError(t, err)
		require.Equal(t, createPb.Slug, getPb.Slug)
		require.Equal(t, createPb.Title, getPb.Title)
	})
}
