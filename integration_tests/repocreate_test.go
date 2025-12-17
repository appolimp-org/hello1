package integrationtests

import (
	"common/utils"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/services/quota"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestRepoCreateDefaultValues() {
	t := suite.T()
	repoSlug := suite.mustGenUniqueSlug()

	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug: utils.PtrFromValue("yandex"),
			Slug:    repoSlug,
			Name:    repoSlug,
		}).Post("/api/v1/repos")).
		MustBe(t, 201)
	require.True(t, repo.Empty)
	require.Equal(t, "", repo.Description)

	var listRepos schemas.Collection[schemas.RepoDetails]
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yandex/repos")).
		MustBe(t, 200)

	for _, r := range listRepos.Result {
		if r.OrgSlug == "yandex" && r.Slug == repoSlug {
			require.Equal(t, "", r.Description)
			break
		}
	}
}

func (suite *RwApiTestSuite) TestRepoCreateWithDescription() {
	t := suite.T()
	repoSlug := suite.mustGenUniqueSlug()

	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug:     utils.PtrFromValue("yandex"),
			Slug:        repoSlug,
			Name:        repoSlug,
			Description: utils.PtrFromValue("Test repo description"),
		}).Post("/api/v1/repos")).
		MustBe(t, 201)
	require.True(t, repo.Empty)
	require.Equal(t, "Test repo description", repo.Description)

	var listRepos schemas.Collection[schemas.RepoDetails]
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yandex/repos")).
		MustBe(t, 200)

	for _, r := range listRepos.Result {
		if r.OrgSlug == "yandex" && r.Slug == repoSlug {
			require.Equal(t, "Test repo description", r.Description)
			break
		}
	}
}

func (suite *RwApiTestSuite) TestRepoCreateWithDefaultQuotaLimit() {
	t := suite.T()
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()
	defaultCreateRepoQuota := int(quota.UnittestDefaults[entities.Quotas.RepositoriesCount])

	// create repos included in default quota
	for i := 0; i < defaultCreateRepoQuota; i++ {
		repoSlug := suite.mustGenUniqueSlug()
		var repo schemas.RepoDetails

		testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
			SetResult(&repo).
			SetBody(&schemas.CreateRepositoryRequest{
				OrgSlug: utils.PtrFromValue("yango"),
				Slug:    repoSlug,
				Name:    repoSlug,
			}).Post("/api/v1/repos")).MustBe(t, 201)

		require.True(t, repo.Empty)
	}

	var listRepos schemas.Collection[schemas.RepoDetails]
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yango/repos")).
		MustBe(t, 200)

	require.Len(t, listRepos.Result, defaultCreateRepoQuota)

	// create repo above default quota
	repoSlug := suite.mustGenUniqueSlug()
	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug: utils.PtrFromValue("yango"),
			Slug:    repoSlug,
			Name:    repoSlug,
		}).Post("/api/v1/repos")).
		MustBe(t, 422)

	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yango/repos")).
		MustBe(t, 200)

	require.Len(t, listRepos.Result, defaultCreateRepoQuota)
}

func (suite *RwApiTestSuite) TestRepoCreateWithCustomQuotaLimit() {
	t := suite.T()
	const customCreateRepoQuota = 10
	suite.repoQuotaChecker.DisableBypass()
	defer suite.repoQuotaChecker.EnableBypass()

	cancel := suite.setQuotaLimit(t, suite.orgs.Yango.ID, entities.Quotas.RepositoriesCount, customCreateRepoQuota)
	defer cancel()

	// create repos included in quota
	for i := 0; i < customCreateRepoQuota; i++ {
		repoSlug := suite.mustGenUniqueSlug()
		var repo schemas.RepoDetails

		testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
			SetResult(&repo).
			SetBody(&schemas.CreateRepositoryRequest{
				OrgSlug: utils.PtrFromValue("yango"),
				Slug:    repoSlug,
				Name:    repoSlug,
			}).Post("/api/v1/repos")).MustBe(t, 201)

		require.True(t, repo.Empty)
	}

	var listRepos schemas.Collection[schemas.RepoDetails]
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yango/repos")).
		MustBe(t, 200)

	require.Len(t, listRepos.Result, customCreateRepoQuota)

	// create repo above default quota
	repoSlug := suite.mustGenUniqueSlug()
	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug: utils.PtrFromValue("yango"),
			Slug:    repoSlug,
			Name:    repoSlug,
		}).Post("/api/v1/repos")).
		MustBe(t, 422)

	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yango/repos")).
		MustBe(t, 200)

	require.Len(t, listRepos.Result, customCreateRepoQuota)
}
