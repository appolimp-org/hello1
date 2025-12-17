package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"testing"
)

func (suite *RepoApiTestSuite) TestOrgs_CreateRepo() {
	t := suite.T()

	org := suite.OrganizationFixture(0, "testorgauth", suite.users.Admin, nil)

	t.Run("not member can't create repo", func(t *testing.T) {
		repoSlug := "repo123"
		var repo schemas.RepoDetails
		testutils.Expect(suite.client.As(testutils.UserIdentities.Slowpoke).
			SetResult(&repo).
			SetBody(&schemas.CreateRepositoryRequest{
				OrgSlug: &org.Slug,
				Slug:    repoSlug,
				Name:    repoSlug,
			}).Post("/api/v1/repos")).
			MustBe(t, 403)
	})

	t.Run("member create repo", func(t *testing.T) {
		suite.addOrgRole(t, suite.users.Krosh, org, iam.Roles.InternalOrganizationManagerMember)

		repoSlug := "repo1234"
		var repo schemas.RepoDetails
		testutils.Expect(suite.client.As(testutils.UserIdentities.Krosh).
			SetResult(&repo).
			SetBody(&schemas.CreateRepositoryRequest{
				OrgSlug: &org.Slug,
				Slug:    repoSlug,
				Name:    repoSlug,
			}).Post("/api/v1/repos")).
			MustBe(t, 201)
		require.True(t, repo.Empty)
	})
}
