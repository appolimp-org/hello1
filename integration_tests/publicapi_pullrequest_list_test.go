package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListPullRequests() {
	t := suite.T()

	// Prepare data in Alpha repo
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "1 - alpha", Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "2 - alpha", Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "3 - alpha", Reviewers: []uint64{suite.users.Barash.ID}})

	// List by repo slug, deterministic order by title
	resp, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{"sort_by": "title"}).
		Get(fmt.Sprintf("/repos/%s/%s/pulls", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug))
	require.NoError(t, err)

	pathIgnore := []string{"**/updated_at", "**/created_at", "**/id"}
	yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
}

func (suite *RwApiTestSuite) TestPublicAPIListPullRequestsByID_Pagination() {
	t := suite.T()

	// Prepare data in Alpha repo (ensure at least 3 PRs exist)
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "p1", Reviewers: []uint64{suite.users.Krosh.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "p2", Reviewers: []uint64{suite.users.Krosh.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "p3", Reviewers: []uint64{suite.users.Krosh.ID}})

	type listPRsResp struct {
		PullRequests  []map[string]any `json:"pull_requests"`
		NextPageToken string           `json:"next_page_token"`
	}

	page1 := &listPRsResp{}
	resp1, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{
			"sort_by":   "created_at",
			"page_size": "1",
		}).
		SetResult(page1).
		Get(fmt.Sprintf("/repos/id:%s/pulls", suite.repos.Alpha.UUID.String()))
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Len(t, page1.PullRequests, 1)
	require.NotEmpty(t, page1.NextPageToken)

	// Next page: omit sort_by, pass page_token
	page2 := &listPRsResp{}
	resp2, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{
			"page_size":  "1",
			"page_token": page1.NextPageToken,
		}).
		SetResult(page2).
		Get(fmt.Sprintf("/repos/id:%s/pulls", suite.repos.Alpha.UUID.String()))
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, page2.PullRequests, 1)

	// Third page, if any
	if page2.NextPageToken != "" {
		page3 := &listPRsResp{}
		resp3, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetQueryParams(map[string]string{
				"page_size":  "1",
				"page_token": page2.NextPageToken,
			}).
			SetResult(page3).
			Get(fmt.Sprintf("/repos/id:%s/pulls", suite.repos.Alpha.UUID.String()))
		require.NoError(t, err)
		require.NotNil(t, resp3)
		require.Len(t, page3.PullRequests, 1)
	}
}

func (suite *RwApiTestSuite) TestPublicAPIListUserPullRequests_BySlug() {
	t := suite.T()

	// Prepare data: create PRs authored and reviewed by specific users in Alpha repo
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "u1 - alpha", Reviewers: []uint64{suite.users.Krosh.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "u2 - alpha", Reviewers: []uint64{suite.users.PinPublic.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "u3 - alpha", Reviewers: []uint64{suite.users.Barash.ID}})

	// List PRs by user slug as author, deterministic order by title
	resp, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{"sort_by": "title", "role": "author"}).
		Get(fmt.Sprintf("/users/%s/pulls", suite.users.Barash.Username))
	require.NoError(t, err)

	pathIgnore := []string{"**/updated_at", "**/created_at", "**/id"}
	yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
}

func (suite *RwApiTestSuite) TestPublicAPIListUserPullRequestsByID_Pagination() {
	t := suite.T()

	// Prepare at least 3 PRs where target user is reviewer to test role selection and pagination
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "ur1", Reviewers: []uint64{suite.users.Barash.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "ur2", Reviewers: []uint64{suite.users.Barash.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "ur3", Reviewers: []uint64{suite.users.Barash.ID}})

	type listPRsResp struct {
		PullRequests  []map[string]any `json:"pull_requests"`
		NextPageToken string           `json:"next_page_token"`
	}

	// First page with sort_by=created_at and page_size=1
	page1 := &listPRsResp{}
	resp1, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{
			"sort_by":   "created_at",
			"page_size": "1",
			"role":      "reviewer",
		}).
		SetResult(page1).
		Get(fmt.Sprintf("/users/id:%s/pulls", suite.users.Barash.UUID.String()))
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Len(t, page1.PullRequests, 1)

	// Second page: omit sort_by, pass page_token
	page2 := &listPRsResp{}
	resp2, err := suite.gwClient.As(suite.users.Admin.Identity).
		SetQueryParams(map[string]string{
			"page_size":  "1",
			"page_token": page1.NextPageToken,
			"role":       "reviewer",
		}).
		SetResult(page2).
		Get(fmt.Sprintf("/users/id:%s/pulls", suite.users.Barash.UUID.String()))
	require.NoError(t, err)
	require.NotNil(t, resp2)
	require.Len(t, page2.PullRequests, 1)

	// Third page if available
	if page2.NextPageToken != "" {
		page3 := &listPRsResp{}
		resp3, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetQueryParams(map[string]string{
				"page_size":  "1",
				"page_token": page2.NextPageToken,
				"role":       "reviewer",
			}).
			SetResult(page3).
			Get(fmt.Sprintf("/users/id:%s/pulls", suite.users.Barash.UUID.String()))
		require.NoError(t, err)
		require.NotNil(t, resp3)
		require.Len(t, page3.PullRequests, 1)
	}
}
