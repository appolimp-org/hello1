package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIForkRepository() {
	t := suite.T()

	// Grant permissions for test users
	suite.addOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex, iam.Roles.RepositoriesMaintainer)
	suite.addOrgRole(t, suite.users.Kopatych, suite.orgs.Smeshariki, iam.Roles.RepositoriesMaintainer)

	tests := []struct {
		name     string
		user     *entities.User
		url      string
		request  *pbPub.ForkRepositoryBody
		expected int
	}{
		{
			name: "to same org using org_slug",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Yandex.Slug,
				Slug:              "fork-same-org",
				DefaultBranchOnly: true,
			},
			expected: http.StatusCreated,
		},
		{
			name: "using org_id",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgId:             suite.orgs.Smeshariki.UUID.String(),
				Slug:              "fork-by-org-id",
				DefaultBranchOnly: true,
			},
			expected: http.StatusCreated,
		},
		{
			name: "repo_id instead of slug",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/id:%s/fork", suite.repos.Alpha.UUID.String()),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Yandex.Slug,
				Slug:              "fork-by-id",
				DefaultBranchOnly: true,
			},
			expected: http.StatusCreated,
		},
		{
			name: "private repo",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.AuthRepoPrivate.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Smeshariki.Slug,
				Slug:              "fork-private",
				DefaultBranchOnly: true,
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "forbidden_source",
			user: suite.users.Krosh,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.AuthRepoPrivate.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug: suite.orgs.Smeshariki.Slug,
				Slug:    "fork-no-permission",
			},
			expected: http.StatusForbidden,
		},
		{
			name: "forbidden_target",
			user: suite.users.Krosh,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug: suite.orgs.Yandex.Slug, // no permission to create repos here
				Slug:    "fork-no-org-permission",
			},
			expected: http.StatusForbidden,
		},
		{
			name: "duplicate_slug",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug: suite.orgs.Yandex.Slug,
				Slug:    suite.repos.Alpha.Slug, // already exists
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "invalid_target",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug: "non-existent-org",
				Slug:    "fork-invalid-org",
			},
			expected: http.StatusNotFound,
		},
		{
			name: "without_org_id_or_org_slug",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				Slug: "fork-invalid-org",
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "empty_slug",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Smeshariki.Slug,
				Slug:              "", // no slug provided, should use source repo slug
				DefaultBranchOnly: true,
			},
			expected: http.StatusCreated,
		},
		{
			name: "all_branches",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Smeshariki.Slug,
				Slug:              "fork-all-branches",
				DefaultBranchOnly: false,
			},
			expected: http.StatusCreated,
		},
		{
			name: "default_branch_only",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, suite.repos.Alpha.Slug),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Smeshariki.Slug,
				Slug:              "fork-default-only",
				DefaultBranchOnly: true,
			},
			expected: http.StatusCreated,
		},
		{
			name: "non-existent_repository",
			user: suite.users.Kopatych,
			url:  fmt.Sprintf("/repos/%s/%s/fork", suite.orgs.Yandex.Slug, "nonexistent"),
			request: &pbPub.ForkRepositoryBody{
				OrgSlug: suite.orgs.Yandex.Slug,
				Slug:    "fork-nonexistent",
			},
			expected: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := suite.gwClient.As(tc.user.Identity).
				SetBody(tc.request).
				Post(tc.url)

			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			//yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp, "**/last_updated", "**/id", "**/fork_origin", "**/request_id")
		})
	}
}
