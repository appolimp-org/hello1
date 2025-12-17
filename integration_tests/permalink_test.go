package integrationtests

import (
	"gitcore/internal/testutils"
	"testing"
)

func (suite *RepoApiTestSuite) TestPermalinks() {
	outerT := suite.T()

	outerT.Run("HTTPS", func(t *testing.T) {
		suite.testPermalinks(t, suite.HTTPSProtocol())
	})

	outerT.Run("SSH", func(t *testing.T) {
		suite.testPermalinks(t, suite.SSHProtocol())
	})
}

func (suite *RepoApiTestSuite) testPermalinks(t *testing.T, protocol Protocol) {
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)
	repoURL := protocol.RepoURL(orgSlug, repoSlug)

	tmpDir := testutils.TempDir(t, "", "permalink")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init")
	cg.Must(t, "remote", "add", "origin", repoURL)
	cg.Must(t, "checkout", "-b", "master")
	w.MkFile("1.txt", "foo*edited")
	cg.Must(t, "add", "1.txt")
	cg.Must(t, "commit", "-m", "asdfg")
	cg.Must(t, "push", "-u", "origin", "master")
	cg.Must(t, "pull")
}
