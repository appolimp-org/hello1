package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListTags() {
	t := suite.T()

	tt := map[string]struct {
		url      string
		user     *entities.User
		params   map[string]string
		expected int
	}{
		"happy_path": {
			url:      fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user:     suite.users.Kopatych,
			expected: http.StatusOK,
		},
		"by_repo_id": {
			url:      fmt.Sprintf("/repos/by-id/%s/tags", suite.repos.ListTags.UUID.String()),
			user:     suite.users.Kopatych,
			expected: http.StatusOK,
		},
		"by_repo_id_prefix": {
			url:      fmt.Sprintf("/repos/id:%s/tags", suite.repos.ListTags.UUID.String()),
			user:     suite.users.Kopatych,
			expected: http.StatusOK,
		},
		"limit": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"page_size": "1",
			},
			expected: http.StatusOK,
		},
		"sort_by_name": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"sort_by": "name",
			},
			expected: http.StatusOK,
		},
		"sort_by_name_reverse": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"sort_by": "-name",
			},
			expected: http.StatusOK,
		},
		"filter": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"filter": "v2",
			},
			expected: http.StatusOK,
		},
		"filter_empty": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"filter": "<nothing>",
			},
			expected: http.StatusOK,
		},
		"filter_escaping": {
			url:  fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"filter": "%25v2",
			},
			expected: http.StatusOK,
		},
		"repo_not_found": {
			url:      fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, "nonexistent"),
			user:     suite.users.Kopatych,
			expected: http.StatusNotFound,
		},
		"forbidden": {
			url:      fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.AuthRepoPrivate.Slug),
			user:     suite.users.Kopatych,
			expected: http.StatusForbidden,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			req := suite.gwClient.As(tc.user.Identity)
			for k, v := range tc.params {
				req = req.SetQueryParam(k, v)
			}

			resp, err := req.Get(tc.url)
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)
			if tc.expected != http.StatusOK {
				return
			}

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp)
		})
	}

	t.Run("pagination", func(t *testing.T) {
		var nextPageToken string

		t.Run("first", func(t *testing.T) {
			resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
				SetQueryParam("page_size", "3").
				Get(fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp)

			nextPageToken, err = yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
			require.NoError(t, err)
			require.NotEmpty(t, nextPageToken)
		})

		t.Run("next", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
				SetQueryParam("page_size", "1").
				SetQueryParam("page_token", nextPageToken).
				Get(fmt.Sprintf("/repos/%s/%s/tags", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp)
		})
	})
}
