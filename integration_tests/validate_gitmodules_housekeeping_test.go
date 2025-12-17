package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/housekeeping"
	"path"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestValidateGitmodulesHousekeeping() {
	t := suite.T()

	repo := suite.repos.Alpha
	author := suite.users.Kopatych

	suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)

	cg, tmpDir := suite.initCGit(author, repo.FullSlug())

	filePath := path.Join(tmpDir, "alpha", ".gitmodules")

	// Create commit with invalid .gitmodules
	commit(t, &cg, filePath, invalidGitmodules)
	cg.Must(t, "push")

	// Create commit with valid .gitmodules
	commit(t, &cg, filePath, goodGitmodules)
	cg.Must(t, "push")

	// Create housekeeping task
	task := housekeeping.NewValidateGitmodules(housekeeping.ValidateGitmodulesDeps{
		RepositoryRepository:      suite.RepoRepo,
		MetadataRepositoryFactory: suite.MetaDataRepoFactory,
		GitFSFactory:              suite.GitFSFactory,
		Pool:                      suite.Pool,
	})

	// Test single repository validation
	t.Run("single_repo", func(t *testing.T) {
		err := task.Invoke(
			"--repo_id", strconv.FormatUint(repo.ID, 10),
		)
		require.NoError(t, err, "housekeeping task should complete without errors")
	})

	// Test with all repositories
	t.Run("all_repos", func(t *testing.T) {
		err := task.Invoke("--all")
		require.NoError(t, err, "housekeeping task should complete without errors when processing all repos")
	})

	// Test pagination with small page sizes
	t.Run("pagination_test", func(t *testing.T) {
		err := task.Invoke(
			"--repo_id", strconv.FormatUint(repo.ID, 10),
			"--commit_page_size", "1",
		)
		require.NoError(t, err, "housekeeping task should complete without errors with small commit page size")

		err = task.Invoke(
			"--all",
			"--repo_page_size", "1",
			"--commit_page_size", "1",
		)
		require.NoError(t, err, "housekeeping task should complete without errors with small page sizes")
	})
}
