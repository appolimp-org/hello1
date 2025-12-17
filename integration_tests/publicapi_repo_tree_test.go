package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListTree() {
	t := suite.T()

	emptyRepoSlug := "empty-repo"
	suite.emptyRepo(suite.orgs.Yandex.Slug, emptyRepoSlug, suite.users.Admin)

	tt := map[string]struct {
		url      string
		user     *entities.User
		params   map[string]string
		expected int
	}{
		"happy_path": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.ListTree.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"by_repo_id": {
			url:  fmt.Sprintf("/repos/by-id/%s/trees", suite.repos.Alpha.UUID.String()),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"by_repo_id_prefix": {
			url:  fmt.Sprintf("/repos/id:%s/trees", suite.repos.Alpha.UUID.String()),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"symlinks": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.SymLink.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"submodule": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.SubmoduleParent.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_revision": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"revision":  "master",
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_revision_commit": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"revision":  "35e85108805c84807bc66a02d91535e1e24b38b9",
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_revision_tag": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.ListTags.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"revision":  "tag:v2.main-an",
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_path": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"path":      "json",
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_path_file": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"path":      "json/short.json",
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"not_found_revision": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"revision":  "not_found_revision",
				"recursive": "true",
			},
			expected: http.StatusNotFound,
		},
		"revision_with_nulls": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"revision":  "branch\x00with\x00nulls",
				"recursive": "true",
			},
			expected: http.StatusBadRequest,
		},
		"repo_not_found": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, "nonexistent"),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusNotFound,
		},
		"forbidden": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.AuthRepoPrivate.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusForbidden,
		},
		"empty_repo": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, emptyRepoSlug),
			user: suite.users.Kopatych,
			params: map[string]string{
				"recursive": "true",
			},
			expected: http.StatusOK,
		},
		"with_page_size_small": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
				"page_size": "5",
			},
			expected: http.StatusOK,
		},
		"with_page_size_large": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
				"page_size": "100",
			},
			expected: http.StatusOK,
		},
		"with_page_size_zero": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
				"page_size": "0",
			},
			expected: http.StatusOK,
		},
		"with_page_size_invalid": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
				"page_size": "invalid",
			},
			expected: http.StatusBadRequest,
		},
		"with_page_size_negative": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "true",
				"page_size": "-1",
			},
			expected: http.StatusBadRequest,
		},
		"non_recursive": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.ListTree.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"recursive": "false",
			},
			expected: http.StatusOK,
		},
		"with_path_and_pagination": {
			url:  fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			user: suite.users.Krosh,
			params: map[string]string{
				"path":      "json",
				"recursive": "true",
				"page_size": "1",
			},
			expected: http.StatusOK,
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
				SetQueryParam("page_size", "10").
				SetQueryParam("path", "foo").
				SetQueryParam("recursive", "true").
				Get(fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.ListTree.Slug))
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
				SetQueryParam("page_size", "1").     // ignore
				SetQueryParam("recursive", "false"). // ignore
				SetQueryParam("page_token", nextPageToken).
				Get(fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.ListTree.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusOK)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp)
		})

		t.Run("invalid_token", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
				SetQueryParam("page_token", nextPageToken).
				Get(fmt.Sprintf("/repos/%s/%s/trees", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug))
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, http.StatusNotFound)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
		})
	})
}
