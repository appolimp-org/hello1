package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	except "gitcore/internal/exceptions"
	"github.com/stretchr/testify/require"
	"path"
)

var maliciousGitmodules = `
[submodule "foo"]
  path = "foo^M"
`

var maliciousGitmodules2 = `
[submodule "foo"]
  path = foo^M
`

var maliciousGitmodules3 = `
[submodule "foo"]
  path = "foo^Moo"
`

var goodGitmodules = `
[submodule "foo"]
  path = "foo"
`

var invalidGitmodules = `
  path = "foo^M"
`

func (suite *RwApiTestSuite) TestValidateGitModules() {
	t := suite.T()

	repo := suite.repos.Alpha
	author := suite.users.Kopatych

	suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)

	cg, tmpDir := suite.initCGit(author, repo.FullSlug())

	filePath := path.Join(tmpDir, "alpha", ".gitmodules")

	commit(t, &cg, filePath, invalidGitmodules)
	cg.Must(t, "push")

	commit(t, &cg, filePath, goodGitmodules)
	cg.Must(t, "push")

	for _, content := range []string{maliciousGitmodules, maliciousGitmodules2, maliciousGitmodules3} {
		commit(t, &cg, filePath, content)
		_, e, err := cg.Exec("push")
		require.Error(t, err)
		require.Contains(t, e, except.SuspiciousContent.Build(".gitmodules").Error())
	}
}
