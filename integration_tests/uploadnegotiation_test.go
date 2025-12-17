package integrationtests

import (
	"common/pkgenerator"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) testSmallPull(t *testing.T, protocol Protocol, caps entities.ProtoCapsFlags) {
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych, caps)
	repoURL := protocol.RepoURL(orgSlug, repoSlug)

	tmpDir := testutils.TempDir(t, "", "conflicting")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	// Create new repo and push
	cg.Must(t, "init")
	cg.Must(t, "remote", "add", "origin", repoURL)
	cg.Must(t, "checkout", "-b", "master")
	w.MkFile("1.txt", "foo*edited")
	cg.Must(t, "add", "1.txt")
	cg.Must(t, "commit", "-m", "asdfg")
	// cg.MustSetOrigin(t, repoURL)
	cg.Must(t, "push", "-u", "origin", "master")

	// Clone in two different folders
	cg.Must(t, "clone", repoURL, "alpha1") //
	cg.Must(t, "clone", repoURL, "alpha2") //

	w1 := w.ChildDir("alpha1")
	cg1 := protocol.PrepareCGit(w1, testutils.UserIdentities.Kopatych)
	cg1.Must(t, "pull")

	w2 := w.ChildDir("alpha2")
	cg2 := protocol.PrepareCGit(w2, testutils.UserIdentities.Kopatych)
	cg2.Must(t, "pull")

	suite.makeNewFile(cg2, "newfile2.txt")
	cg2.Must(t, "add", "newfile2.txt")
	cg2.Must(t, "commit", "-m", "newfile2")
	cg2.Must(t, "push", "-u", "origin", "master")

	for i := 1; i < 3; i++ {
		branchName := fmt.Sprintf("branch%d", i)
		cg1.Must(t, "checkout", "-b", branchName)
		for j := 1; j < 3; j++ {
			fname := fmt.Sprintf("%s_%d.txt", branchName, j)
			suite.makeNewFile(cg1, fname)
			cg1.Must(t, "add", fname)
			cg1.Must(t, "commit", "-m", "___")
		}
		cg1.Must(t, "push", "-u", "origin", branchName)
	}

	suite.makeNewFile(cg2, "newfile3.txt")
	for j := 1; j < 3; j++ {
		fname := fmt.Sprintf("master_%d.txt", j)
		suite.makeNewFile(cg2, fname)
		cg2.Must(t, "add", fname)
		cg2.Must(t, "commit", "-m", "+++")
	}

	cg2.Must(t, "pull")
}

func (suite *RepoApiTestSuite) TestSmallPull() {
	outerT := suite.T()

	outerT.Run("HTTPS", func(t *testing.T) {
		suite.testSmallPull(t, suite.HTTPSProtocol(), entities.EnableAll)
	})

	outerT.Run("HTTPS no-multiack", func(t *testing.T) {
		suite.testSmallPull(t, suite.HTTPSProtocol(), entities.DisableMultiAck)
	})

	outerT.Run("HTTPS no-nodone", func(t *testing.T) {
		suite.testSmallPull(t, suite.HTTPSProtocol(), entities.DisableNodone)
	})

	outerT.Run("SSH", func(t *testing.T) {
		suite.testSmallPull(t, suite.SSHProtocol(), entities.EnableAll)
	})

	outerT.Run("SSH no-multiack", func(t *testing.T) {
		suite.testSmallPull(t, suite.SSHProtocol(), entities.DisableMultiAck)
	})

	outerT.Run("SSH no-multiack-detailed", func(t *testing.T) {
		suite.testSmallPull(t, suite.SSHProtocol(), entities.DisableMultiAckDetailed)
	})
}

func (suite *RepoApiTestSuite) testMultiAck(t *testing.T, protocol Protocol, repoURL string) {
	tmpDir := testutils.TempDir(t, "", "multiack")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	cg.Must(t, "init")
	cg.Must(t, "remote", "add", "origin", repoURL)

	// Create initial root commit
	suite.makeNewFile(cg, "fff1.txt")
	cg.Must(t, "add", "fff1.txt")
	cg.Must(t, "commit", "-m", "fff1.txt")

	var bnames []string
	for i := 0; i < 16; i++ {
		bnames = append(bnames, fmt.Sprintf("br%04d", i))
	}

	// Then, make branches
	for _, branch := range bnames {
		cg.Must(t, "checkout", "-b", branch)
	}

	// Make an initial repo state
	for mi, branch := range bnames {
		cg.Must(t, "checkout", branch)
		for i := 0; i <= mi; i++ {
			fname := fmt.Sprintf("%s%04d", branch, i)
			suite.makeNewFile(cg, fname)

			cg.Must(t, "add", fname)
			cg.Must(t, "commit", "-m", fname)

			if (mi*i)%5 == 2 {
				// Introduce some merges
				cg.Must(t, "merge", bnames[(2*mi-i)%len(bnames)])
			}
		}
	}

	// Force is needed only for gitlab/github repos which are not empty
	cg.Must(t, "push", "--all", "--force", "origin")

	// Now, create a git repo that knows this root commit only for both branches
	w2 := testutils.NewWorkdir(t, tmpDir)
	cg2 := protocol.PrepareCGit(w2, testutils.UserIdentities.Kopatych)
	cg2.Must(t, "clone", repoURL, "dd")
	cg2 = protocol.PrepareCGit(w2.ChildDir("dd"), testutils.UserIdentities.Kopatych)
	// Need to checkout branch before pulling at least for git version 2.37.1 (Apple Git-137.1)
	cg2.Must(t, "checkout", bnames[0])
	cg2.Must(t, "pull")

	// Create new branches
	newbranches := []string{"newb1", "newb2"}
	for mi, branch := range newbranches {
		cg.Must(t, "checkout", "-b", branch, bnames[mi]+"^")
		fname := fmt.Sprintf("%s%d", branch, 1)
		suite.makeNewFile(cg, fname)

		cg.Must(t, "add", fname)
		cg.Must(t, "commit", "-m", fname)
	}

	// And fill them with commits
	for _, branch := range bnames {
		cg.Must(t, "checkout", branch)
		for i := 1021; i < 1022; i++ {
			fname := fmt.Sprintf("%s%d", branch, i)
			suite.makeNewFile(cg, fname)

			cg.Must(t, "add", fname)
			cg.Must(t, "commit", "-m", fname)
		}
	}
	// And update the repo
	cg.Must(t, "push", "--all", "origin")

	// The last step - pull it from the repo that is far behind
	cg2.Must(t, "pull")
}

func (suite *RepoApiTestSuite) TestMultiAck() {
	outerT := suite.T()

	testWithProtocolAndCaps := func(t *testing.T, protocol Protocol, caps ...entities.ProtoCapsFlags) {
		_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych, caps...)
		repoURL := protocol.RepoURL(orgSlug, repoSlug)

		suite.testMultiAck(t, protocol, repoURL)
	}

	outerT.Run("HTTPS", func(t *testing.T) {
		testWithProtocolAndCaps(t, suite.HTTPSProtocol(), entities.EnableAll)
	})

	outerT.Run("HTTPS wo nodone", func(t *testing.T) {
		testWithProtocolAndCaps(t, suite.HTTPSProtocol(),
			entities.DisableNodone)
	})

	outerT.Run("HTTPS wo multiack", func(t *testing.T) {
		t.Skip("This fails as vanilla GIT can't process this scenario w/o multi_ack")
		testWithProtocolAndCaps(t, suite.HTTPSProtocol(),
			entities.DisableMultiAck, entities.DisableMultiAckDetailed, entities.DisableNodone)
	})

	outerT.Run("SSH", func(t *testing.T) {
		testWithProtocolAndCaps(t, suite.SSHProtocol(), entities.EnableAll)
	})

	outerT.Run("SSH wo multiack", func(t *testing.T) {
		testWithProtocolAndCaps(t, suite.SSHProtocol(), entities.DisableMultiAck, entities.DisableMultiAckDetailed)
	})
}

func (suite *IntegrationTestSuite) makeRandomRepo(t *testing.T, owner *entities.User, caps ...entities.ProtoCapsFlags) (
	repoID uint64,
	orgSlug string,
	repoSlug string,
) {
	return suite.makeRepoWithVisibility(t, owner, entities.Visibilities.Public, caps...)
}

func (suite *IntegrationTestSuite) makeRepoWithVisibility(t *testing.T,
	owner *entities.User, visibility entities.Visibility, caps ...entities.ProtoCapsFlags) (
	repoID uint64,
	orgSlug string,
	repoSlug string,
) {
	ctx := context.Background()

	id, err := pkgenerator.GetNextID()
	require.NoError(t, err)

	orgSlug = fmt.Sprintf("org%d", id)
	orgSchema := suite.OrganizationFixture(0, orgSlug, suite.users.Admin, nil)
	org, err := suite.OrgRepo.GetOrganization(ctx, orgSchema.Slug)
	require.NoError(t, err)

	repoSlug = fmt.Sprintf("repo%d", id)

	var cp entities.ProtoCapsFlags
	cp.Set(caps...)
	repopar := interfaces.CreateRepositoryArgs{
		Name:         repoSlug,
		Slug:         repoSlug,
		OrgID:        org.ID,
		CreatedBy:    owner.ID,
		ProtocolCaps: cp,
		Visibility:   visibility,
	}

	repoID, err = suite.RepoRepo.CreateRepository(ctx, &repopar)
	require.NoError(t, err)

	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	suite.addRole(t, owner, repo, iam.Roles.RepositoriesAdmin)
	return repoID, orgSlug, repoSlug
}
