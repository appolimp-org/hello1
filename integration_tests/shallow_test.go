package integrationtests

import (
	"fmt"
	"gitcore/internal/testutils"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

var receivingObjects = regexp.MustCompile(`Packing objects:\s*\d*\s*of\s*(\d*)`)

func findReceivedObjectsFromLog(t *testing.T, l string) int {
	if !receivingObjects.MatchString(l) {
		return 0
	}
	res := receivingObjects.FindStringSubmatch(l)
	require.GreaterOrEqual(t, len(res), 2)
	v, err := strconv.ParseInt(res[len(res)-1], 10, 32)
	require.NoError(t, err)
	return int(v)
}

func (suite *RepoApiTestSuite) TestShallowSmoke() {
	t := suite.T()
	protocol := suite.SSHProtocol()

	repoURL := fmt.Sprintf("%s%s/%s.git", protocol.Host, "yandex", "alpha")
	tmpDir := testutils.TempDir(t, "", "shallow")
	w := testutils.NewWorkdir(t, tmpDir)

	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	_, e := cg.MustAndLog(t, "clone", repoURL, "--depth=1", "--progress", "d1")
	require.Equal(t, 15, findReceivedObjectsFromLog(t, e))

	w2 := w.ChildDir("d1")

	cg2 := protocol.PrepareCGit(w2, testutils.UserIdentities.Kopatych)

	_, e2 := cg2.MustAndLog(t, "pull")
	require.Zero(t, findReceivedObjectsFromLog(t, e2))
	cg2.MustHaveLogDepth(t, 1)

	_, e3 := cg2.MustAndLog(t, "pull", "--progress", "--depth=2")
	require.Equal(t, 3, findReceivedObjectsFromLog(t, e3))
	cg2.MustHaveLogDepth(t, 2)

	_, org, repo := suite.makeRandomRepo(t, suite.users.Kopatych)
	cloneURL := protocol.RepoURL(org, repo)

	cgfull := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)
	cgfull.MustAndLog(t, "clone", repoURL, "--progress", "d2")

	cgfull = protocol.PrepareCGit(w.ChildDir("d2"), testutils.UserIdentities.Kopatych)
	cgfull.Must(t, "remote", "add", "cl", cloneURL)
	cgfull.Must(t, "push", "--all", "cl")

	tmpDirCl := testutils.TempDir(t, "", "shallow_clone")
	wcl := testutils.NewWorkdir(t, tmpDirCl)
	cgcl := protocol.PrepareCGit(wcl, testutils.UserIdentities.Kopatych)
	cgcl.MustAndLog(t, "clone", cloneURL, "--depth=1", "--progress", "d1")
	cgcl = protocol.PrepareCGit(wcl.ChildDir("d1"), testutils.UserIdentities.Kopatych)

	suite.makeNewFile(cgcl, "new.txt")
	cgcl.MustAndLog(t, "add", "new.txt")
	cgcl.MustAndLog(t, "commit", "-m", "new")
	cgcl.MustAndLog(t, "push", "--all")
}

func (suite *RepoApiTestSuite) TestShallowClone() {
	t := suite.T()

	runTest := func(t *testing.T, proto Protocol) {
		repoURL := proto.RepoURL("yandex", "alpha")
		// repoURL := "https://github.com/travelpolicy/testrepo.git"

		tmpDir := testutils.TempDir(t, "", "shallow")
		w := testutils.NewWorkdir(t, tmpDir)

		t.Run("Numeric depth count sent "+proto.Name, func(t *testing.T) {
			cg := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			_, e := cg.MustAndLog(t, "clone", repoURL, "--depth=1", "--progress", "d1")
			require.Equal(t, 15, findReceivedObjectsFromLog(t, e))

			_, e2 := cg.MustAndLog(t, "clone", repoURL, "--depth=2", "--progress", "d2")
			require.Equal(t, 17, findReceivedObjectsFromLog(t, e2))
		})

		t.Run("Numeric depth "+proto.Name, func(t *testing.T) {
			cg := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			cg.Must(t, "clone", repoURL, "--depth=1")

			w2 := w.ChildDir("alpha")

			cg2 := proto.PrepareCGit(w2, testutils.UserIdentities.Kopatych)

			cg2.Must(t, "pull")
			cg2.MustHaveLogDepth(t, 1)

			cg2.Must(t, "pull", "--depth=2")

			cg2.Must(t, "pull", "--depth=4")
			cg2.MustHaveLogDepth(t, 4)

			cg2.Must(t, "pull", "--depth=1")
			cg2.MustHaveLogDepth(t, 1)

			cg2.Must(t, "pull", "--unshallow")
			cg2.MustHaveLogDepth(t, 8)
		})

		t.Run("Time-based depth "+proto.Name, func(t *testing.T) {
			cg := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			cg.Must(t, "clone", repoURL, "--shallow-since=\"2015-03-31T15:52:54Z\"", "timedepth")
			w.ChildDir("timedepth").CGit().MustHaveLogDepth(t, 2)
		})

		t.Run("Ref-based depth "+proto.Name, func(t *testing.T) {
			cg := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			cg.Must(t, "clone", "-b", "master", repoURL,
				"--shallow-exclude=branch",
				"refdepth")
			w.ChildDir("refdepth").CGit().MustHaveLogDepth(t, 1)
		})
		t.Run("Unshallow "+proto.Name, func(t *testing.T) {
			cg := proto.PrepareCGit(w, testutils.UserIdentities.Kopatych)

			cg.Must(t, "clone", repoURL, "--depth=1", "unshallow")
			w2 := w.ChildDir("unshallow")
			cg2 := proto.PrepareCGit(w2, testutils.UserIdentities.Kopatych)
			cg2.MustHaveLogDepth(t, 1)

			cg2.Must(t, "fetch", "origin", "refs/heads/branch:refs/heads/branch")
			cg2.MustHaveLogDepth(t, 1)

			cg2.Must(t, "checkout", "branch")
			cg2.MustHaveLogDepth(t, 8)
		})
	}

	t.Run("SSH", func(t *testing.T) {
		runTest(t, suite.SSHProtocol())
	})

	t.Run("HTTP", func(t *testing.T) {
		runTest(t, suite.HTTPSProtocol())
	})
}
