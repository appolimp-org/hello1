package integrationtests

import (
	"context"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestEmptyRepoFlag() {
	t := suite.T()
	ctx := context.Background()

	orgSlug := "gitcoreorg"
	suite.OrganizationFixture(0, orgSlug, suite.users.Kopatych, nil)
	suite.emptyRepo(orgSlug, "gitcoreempty", suite.users.Kopatych)

	tmpDir := testutils.TempDir(t, "", "testemptyrepo")
	w := testutils.NewWorkdir(t, tmpDir)
	protocol := suite.SSHProtocol()
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master") // default branch name depends on git version, so specify it explicitly
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, "gitcoreempty"))
	fname := "test.txt"
	suite.makeNewFile(cg, fname)
	cg.Must(t, "add", "*")
	cg.Must(t, "commit", "-m", "cmt")

	r1db, err := suite.RepoRepo.GetRepository(ctx, orgSlug, "gitcoreempty")
	require.True(t, r1db.IsEmpty)
	require.True(t, r1db.Flags.IsEmpty)
	require.Nil(t, r1db.DefaultBranch)
	require.NoError(t, err)

	cg.Must(t, "push", "-u", "origin", "master")

	r1db, err = suite.RepoRepo.GetRepository(ctx, orgSlug, "gitcoreempty")
	require.False(t, r1db.IsEmpty)
	require.False(t, r1db.Flags.IsEmpty)
	require.Equal(t, "master", *r1db.DefaultBranch)
	require.NoError(t, err)
}
