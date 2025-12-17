package integrationtests

import (
	"common/functools"
	"context"
	"gitcore/internal/adapters/postgres"
	"gitcore/internal/interfaces"
	"github.com/stretchr/testify/require"
)

func (suite *IntegrationTestSuite) getHousekeeperByName(name string) interfaces.HousekeepingMethod {
	methods := functools.Filter(suite.HousekeepingMethods, func(method interfaces.HousekeepingMethod) bool {
		return method.Name() == name
	})
	require.Len(suite.T(), methods, 1)
	return methods[0]
}

func (suite *RepoApiTestSuite) TestHousekeeping_MigrateRepoFlags() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	_, err := suite.Pool.Exec(context.Background(), "UPDATE git_repos SET is_empty = true, is_migrating = true, flags = '{}'::jsonb WHERE id = $1", repoID)
	require.NoError(t, err)

	housekeeper := suite.getHousekeeperByName("migrate-repo-flags")
	require.NoError(t, housekeeper.Invoke())
}

func (suite *RepoApiTestSuite) TestHousekeeping_FillReleasesRepoID() {
	t := suite.T()

	repo1ID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	repo2ID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	_, err := suite.Pool.Exec(context.Background(), `
		INSERT INTO components (id, repo_id, name, description, tag_prefix, created_at, updated_at, is_deleted, path) VALUES 
			(1, $1, 'Comp1', '', 'c1-', now(), now(), true, ''),
			(2, $2, 'Comp2', '', 'c2-', now(), now(), false, '');
	`, repo1ID, repo2ID)
	require.NoError(t, err)

	_, err = suite.Pool.Exec(context.Background(), `
		INSERT INTO releases (id, component_id, tag, release_notes, changelog, status, created_at, updated_at, is_deleted, repo_id, title) VALUES 
			(1, 1, 'a', '', '', 'draft', now(), now(), true, null, 'Title'),
			(2, 1, 'b', '', '', 'draft', now(), now(), false, null, 'Title'),
			(3, 2, 'c', '', '', 'draft', now(), now(), false, null, 'Title'),
			(4, null, 'd', '', '', 'draft', now(), now(), false, $1, 'Title');
	`, repo2ID)
	require.NoError(t, err)

	housekeeper := suite.getHousekeeperByName("fill-releases-repo-id")
	require.NoError(t, housekeeper.Invoke())

	releaseRepo := postgres.NewReleaseRepositoryWithSoftDeleted(suite.GormDB, suite.TxManager)
	for releaseID, repoID := range map[uint64]uint64{
		1: repo1ID,
		2: repo1ID,
		3: repo2ID,
		4: repo2ID,
	} {
		release, err := releaseRepo.Get(context.Background(), releaseID)
		require.NoError(t, err, releaseID)
		require.Equal(t, repoID, release.RepoID, releaseID)
	}
}
