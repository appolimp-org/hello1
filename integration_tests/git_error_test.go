package integrationtests

import (
	"common/cgit"
	"context"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/httpserver/gitserver"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func (suite *RepoApiTestSuite) TestGitErrorHandling() {
	t := suite.T()

	t.Run("404", func(t *testing.T) {
		name := strings.SplitAfter(t.Name(), "/")
		repoSlug := "giterr-" + name[len(name)-1]
		repoURL, repoID := suite.emptyRepo(suite.orgs.Yandex.Slug, repoSlug, suite.users.Admin)
		tmpDir := testutils.TempDir(t, "", "giterr")
		w := testutils.NewWorkdir(t, tmpDir)

		ctx := context.Background()

		protocol := suite.HTTPSProtocol()
		cg := protocol.PrepareCGit(w, testutils.UserIdentities.Admin)

		cg.Must(t, "init", ".")
		cg.Must(t, "branch", "-m", "master")
		cg.Must(t, "remote", "add", "origin", repoURL)

		suite.makeNewFile(cg, "test1.txt")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "cmt")

		cg.Must(t, "push", "-u", "origin", "master")
		_ = ctx
		_ = repoID

		remoteURL := suite.HTTPSProtocol().RepoURLByID(12345)
		err := cg.MirrorPushRepo(ctx, cg.Path()+"/.git", remoteURL)
		require.ErrorIs(t, err, cgit.ErrRepositoryNotFound)
	})

	t.Run("quota", func(t *testing.T) {
		name := strings.SplitAfter(t.Name(), "/")
		repoSlug := "giterr" + name[len(name)-1]

		revert1 := suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.PackFileSize, 0)
		defer func() {
			revert1()
		}()

		repoURL, _ := suite.emptyRepo(suite.orgs.Yandex.Slug, repoSlug, suite.users.Admin)
		tmpDir := testutils.TempDir(t, "", "giterr")
		w := testutils.NewWorkdir(t, tmpDir)

		ctx := context.Background()

		protocol := suite.HTTPSProtocol()
		cg := protocol.PrepareCGit(w, testutils.UserIdentities.Admin)
		cg.AddExtraHeader(gitserver.SourcecraftErrorHeader, string(entities.ErrorFormats.JSONBase64))

		cg.Must(t, "init", ".")
		cg.Must(t, "branch", "-m", "master")
		cg.Must(t, "remote", "add", "origin", repoURL)

		suite.makeNewFile(cg, "test1.txt")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "cmt")

		err := cg.MirrorPushRepo(ctx, cg.Path()+"/.git", repoURL)
		require.ErrorIs(t, err, except.QuotaLimitExceeded)
	})

}
