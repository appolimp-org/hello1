package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"github.com/stretchr/testify/require"
	"path"
)

func (suite *RwApiTestSuite) TestNewBranchHint() {
	t := suite.T()

	repo := suite.repos.Alpha
	author := suite.users.Kopatych

	suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)

	cg, tmpDir := suite.initCGit(author, repo.FullSlug())

	filePath := path.Join(tmpDir, "alpha", "newfile.go")
	commit(t, &cg, filePath, content1)

	cg.Must(t, "push")
	cg.Must(t, "checkout", "-b", "feature/new_branch")

	commit(t, &cg, path.Join(tmpDir, "alpha", "README"), "# README\n")
	commit(t, &cg, filePath, content2)

	out, e, err := cg.Exec("push", "-u", "origin", "feature/new_branch")
	require.NoErrorf(t, err, "Stdout: %s, Stderr: %s", out, e)
	require.Contains(t, e, "yandex/alpha/create-pr")

	// update branch, no PRs:

	cg.Must(t, "checkout", "branch")

	commit(t, &cg, path.Join(tmpDir, "alpha", "data.txt"), "2+2=4\n")
	out, e, err = cg.Exec("push")
	require.NoErrorf(t, err, "Stdout: %s, Stderr: %s", out, e)
	require.Contains(t, e, "yandex/alpha/create-pr")

	// update branch, PRs:
	commit(t, &cg, path.Join(tmpDir, "alpha", "data2.txt"), "2+2=4!\n")

	suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:   repo,
		Source: "branch",
		Target: "master",
	})

	out, e, err = cg.Exec("push")
	require.NoErrorf(t, err, "Stdout: %s, Stderr: %s", out, e)
	require.Contains(t, e, "yandex/alpha/pr/1")
}
