package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListOrganizationRepositories() {
	t := suite.T()

	// Create test repositories using existing helper
	var kroshPrivateRepos []uint64
	var kroshPublicRepos []uint64
	var kopatychPublicRepos []uint64

	kroshOrg, err := suite.Params.OrgService.GetPersonalOrganization(context.Background(), nil, suite.users.Krosh)
	require.NoError(t, err)

	kopatychOrg, err := suite.Params.OrgService.GetPersonalOrganization(context.Background(), nil, suite.users.Kopatych)
	require.NoError(t, err)

	suite.populateRepos(t, []string{"krosh-private-1", "krosh-private-2"}, entities.Visibilities.Private, kroshOrg.ID, suite.users.Krosh, false, &kroshPrivateRepos)
	suite.populateRepos(t, []string{"krosh-public-1", "krosh-public-2", "krosh-public-3"}, entities.Visibilities.Public, kroshOrg.ID, suite.users.Krosh, false, &kroshPublicRepos)
	suite.populateRepos(t, []string{"kopatych-public-1", "kopatych-public-2"}, entities.Visibilities.Public, kopatychOrg.ID, suite.users.Kopatych, false, &kopatychPublicRepos)

	testCases := []struct {
		name     string
		user     *entities.User
		url      string
		params   map[string]string
		expected int
		hasToken bool
	}{
		{
			name:     "by_org_slug_member",
			user:     suite.users.Krosh,
			url:      fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug),
			expected: http.StatusOK,
		},
		{
			name:     "by_org_slug_non_member",
			user:     suite.users.Kopatych,
			url:      fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug),
			expected: http.StatusOK,
		},
		{
			name:     "by_org_id_member",
			user:     suite.users.Krosh,
			url:      fmt.Sprintf("/orgs/id:%s/repos", kroshOrg.UUID.String()),
			expected: http.StatusOK,
		},
		{
			name:     "by_org_id_non_member",
			user:     suite.users.Kopatych,
			url:      fmt.Sprintf("/orgs/id:%s/repos", kroshOrg.UUID.String()),
			expected: http.StatusOK,
		},
		{
			name:     "with_page_size",
			user:     suite.users.Krosh,
			url:      fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug),
			params:   map[string]string{"page_size": "1"},
			expected: http.StatusOK,
			hasToken: true,
		},
		{
			name:     "non_existent_org",
			user:     suite.users.Krosh,
			url:      "/orgs/non-existent-org/repos",
			expected: http.StatusNotFound,
		},
		{
			name:     "invalid_page_size",
			user:     suite.users.Krosh,
			url:      fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug),
			params:   map[string]string{"page_size": "invalid"},
			expected: http.StatusBadRequest,
		},
		{
			name:     "negative_page_size",
			user:     suite.users.Krosh,
			url:      fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug),
			params:   map[string]string{"page_size": "-1"},
			expected: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := suite.gwClient.As(tc.user.Identity).SetQueryParams(tc.params)

			resp, err := req.Get(tc.url)
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/request_id", "**/next_page_token")

			if tc.hasToken {
				nextPageToken, err := yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
				require.NoError(t, err)
				require.NotEmpty(t, nextPageToken)
			}
		})
	}

	t.Run("pagination", func(t *testing.T) {
		var nextPageToken string

		t.Run("first_page", func(t *testing.T) {
			resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
				SetQueryParam("page_size", "3").
				Get(fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/next_page_token")

			nextPageToken, err = yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
			require.NoError(t, err)
			require.NotEmpty(t, nextPageToken)
		})

		t.Run("second_page", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
				SetQueryParam("page_token", nextPageToken).
				Get(fmt.Sprintf("/orgs/%s/repos", kroshOrg.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/next_page_token")
		})
	})
}

func (suite *RwApiTestSuite) TestPublicAPIDiscoverRepositories() {
	t := suite.T()

	// Create test repositories using existing helper
	var kroshPrivateRepos []uint64
	var kroshPublicRepos []uint64
	var kopatychPublicRepos []uint64

	kroshOrg, err := suite.Params.OrgService.GetPersonalOrganization(context.Background(), nil, suite.users.Krosh)
	require.NoError(t, err)

	kopatychOrg, err := suite.Params.OrgService.GetPersonalOrganization(context.Background(), nil, suite.users.Kopatych)
	require.NoError(t, err)

	suite.populateRepos(t, []string{"krosh-private-discover-1", "krosh-private-discover-2"}, entities.Visibilities.Private, kroshOrg.ID, suite.users.Krosh, false, &kroshPrivateRepos)
	suite.populateRepos(t, []string{"krosh-public-discover-1", "krosh-public-discover-2", "krosh-public-discover-3"}, entities.Visibilities.Public, kroshOrg.ID, suite.users.Krosh, false, &kroshPublicRepos)
	suite.populateRepos(t, []string{"kopatych-public-discover-1", "kopatych-public-discover-2"}, entities.Visibilities.Public, kopatychOrg.ID, suite.users.Kopatych, false, &kopatychPublicRepos)

	testCases := []struct {
		name     string
		user     *entities.User
		params   map[string]string
		expected int
		hasToken bool
	}{
		{
			name:     "as_krosh",
			user:     suite.users.Krosh,
			expected: http.StatusOK,
		},
		{
			name:     "as_kopatych",
			user:     suite.users.Kopatych,
			expected: http.StatusOK,
		},
		{
			name:     "with_page_size",
			user:     suite.users.Krosh,
			params:   map[string]string{"page_size": "2"},
			expected: http.StatusOK,
			hasToken: true,
		},
		{
			name:     "invalid_page_size",
			user:     suite.users.Krosh,
			params:   map[string]string{"page_size": "invalid"},
			expected: http.StatusBadRequest,
		},
		{
			name:     "negative_page_size",
			user:     suite.users.Krosh,
			params:   map[string]string{"page_size": "-1"},
			expected: http.StatusBadRequest,
		},
		{
			name:     "page_size_too_large",
			user:     suite.users.Krosh,
			params:   map[string]string{"page_size": "101"},
			expected: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := suite.gwClient.As(tc.user.Identity).SetQueryParams(tc.params)

			resp, err := req.Get("/repos")
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			if tc.expected == http.StatusOK {
				//yarequire.HTTPDumpFixture(t, resp)
				yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/request_id", "**/next_page_token")

				if tc.hasToken {
					nextPageToken, err := yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
					require.NoError(t, err)
					require.NotEmpty(t, nextPageToken)
				}
			}
		})
	}

	t.Run("pagination", func(t *testing.T) {
		var nextPageToken string

		t.Run("first_page", func(t *testing.T) {
			resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
				SetQueryParam("page_size", "3").
				Get("/repos")
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/next_page_token")

			nextPageToken, err = yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
			require.NoError(t, err)
			require.NotEmpty(t, nextPageToken)
		})

		t.Run("second_page", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
				SetQueryParam("page_token", nextPageToken).
				Get("/repos")
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/next_page_token")
		})
	})

	t.Run("only_public_repos_returned", func(t *testing.T) {
		// Request as Kopatych who should only see public repos (not Krosh's private ones)
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetQueryParam("page_size", "100").
			Get("/repos")
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)

		// Verify response contains only public repositories
		repos, err := yarequire.GetArrayFromJSON(resp.Body(), "repositories")
		require.NoError(t, err)

		// Should not contain any private repositories
		for _, repo := range repos {
			visibility, err := yarequire.GetStringFromJSON(repo, "visibility")
			require.NoError(t, err)
			require.Equal(t, "public", visibility, "Only public repositories should be returned")
		}
	})

	t.Run("filter_by_created_at", func(t *testing.T) {
		before := "2024-12-31T23:59:59Z"
		after := "2030-01-01T00:00:00Z"

		testCases := []struct {
			name   string
			filter string
			status int
		}{
			{
				name:   "created_after",
				filter: "created_at > " + before,
				status: http.StatusOK,
			},
			{
				name:   "created_before",
				filter: "created_at < " + before,
				status: http.StatusOK,
			},
			{
				name:   "created_in_range",
				filter: "created_at > " + before + " AND created_at < " + after,
				status: http.StatusOK,
			},
			{
				name:   "created_in_range_outer",
				filter: "created_at < " + before + " AND created_at > " + after,
				status: http.StatusOK,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
					SetQueryParam("filter", tc.filter).
					SetQueryParam("page_size", "10").
					Get("/repos")
				require.NoError(t, err)
				yarequire.StatusCode(t, resp, err, tc.status)

				if tc.status == http.StatusOK {
					//yarequire.HTTPDumpFixture(t, resp)
					yarequire.HTTPCompareWithFixture(t, resp, "**/id", "**/last_updated", "**/request_id", "**/next_page_token")
				}
			})
		}
	})
}
