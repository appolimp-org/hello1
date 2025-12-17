package integrationtests

import (
	"common/cgit"
	"common/grpc"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"gitcore/pkg/pagination"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestContributors_Push() {
	t := suite.T()
	ctx := context.Background()

	repoSlug := "test-contributors"
	orgSlug := "yandex"

	repoURL, _ := suite.emptyRepo(orgSlug, repoSlug, suite.users.Admin)
	repo, err := suite.RepoRepo.GetRepository(ctx, orgSlug, repoSlug)
	require.NoError(t, err)

	suite.addRole(t, suite.users.Barash, repo, iam.Roles.RepositoriesMaintainer)
	suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesMaintainer)

	contributors, err := suite.RepoContributorRepo.ListContributors(ctx, repo.ID, pagination.Options{})
	require.NoError(t, err)
	require.Equal(t, 0, len(contributors.Result))

	tmpDir := testutils.TempDir(t, "", "")
	repoPath := path.Join(tmpDir, repoSlug)
	adminToken := testutils.FakeIAMAuthToken(testutils.UserIdentities.Admin)

	cg := cgit.NewCGit(tmpDir).WithAuthToken(adminToken)
	cg.Must(t, "clone", repoURL)

	// fix for case when `main` is set as default branch on machine
	cg = cgit.NewCGit(repoPath)
	defaultBranch, _, err := cg.Exec("config", "get", "init.defaultBranch")
	defaultBranch, _ = strings.CutSuffix(defaultBranch, "\n")
	if err != nil {
		// ignore the error, probably just means that it wasn't set up in config, either way irrelevant
		defaultBranch = "master"
	}

	t.Run("User pushes to repo and becomes a contributor", func(t *testing.T) {
		cg = cgit.NewCGit(repoPath).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Barash))

		suite.makeNewFile(cg, "a")
		cg.Must(t, "add", "a")
		cg.Must(t, "commit", "-m", "\"commit\"")
		cg.Must(t, "push")

		// Barash became a contributor
		contributors, err = suite.RepoContributorRepo.ListContributors(ctx, repo.ID, pagination.Options{})
		require.NoError(t, err)
		require.Equal(t, 1, len(contributors.Result))
		require.Equal(t, suite.users.Barash.ID, contributors.Result[0].ID)
	})

	t.Run("User pushes to repo again, nothing fails", func(t *testing.T) {
		cg = cgit.NewCGit(repoPath).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Barash))

		suite.makeNewFile(cg, "b")
		cg.Must(t, "add", "b")
		cg.Must(t, "commit", "-m", "\"commit\"")
		cg.Must(t, "push")

		// Barash is still a contributor
		contributors, err = suite.RepoContributorRepo.ListContributors(ctx, repo.ID, pagination.Options{})
		require.NoError(t, err)
		require.Equal(t, 1, len(contributors.Result))
		require.Equal(t, suite.users.Barash.ID, contributors.Result[0].ID)
	})

	t.Run("Another user pushes to repo and becomes a contributor", func(t *testing.T) {
		cg = cgit.NewCGit(repoPath).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Krosh))

		suite.makeNewFile(cg, "c")
		cg.Must(t, "add", "c")
		cg.Must(t, "commit", "-m", "\"commit\"")
		cg.Must(t, "push")

		// Krosh became a contributor
		contributors, err = suite.RepoContributorRepo.ListContributors(ctx, repo.ID, pagination.Options{})
		require.NoError(t, err)
		require.Equal(t, 2, len(contributors.Result))

		require.Equal(t, suite.users.Krosh.ID, contributors.Result[0].ID)
		require.Equal(t, suite.users.Barash.ID, contributors.Result[1].ID)
	})

	t.Run("Robot is not a contributor", func(t *testing.T) {
		cg = cgit.NewCGit(repoPath).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Krosh))

		cg.Must(t, "checkout", "-b", "feature")
		suite.makeNewFile(cg, "another-file")
		cg.Must(t, "add", "another-file")
		cg.Must(t, "commit", "-m", "\"commit\"")
		cg.Must(t, "push", "--set-upstream", "origin", "feature")

		auth := testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh)
		c := pb.NewPRServiceClient(suite.grpcClient)

		pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:    repo,
			Title:   "PR#1",
			Source:  "feature",
			Target:  defaultBranch,
			Publish: utils.PtrFromValue(true),
		})

		_, err = c.Merge(auth, &pb.MergeRequest{
			PrId:  grpc.MarshalID(pr.ID),
			Force: true,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

		// Robot should not be a contributor
		contributors, err = suite.RepoContributorRepo.ListContributors(ctx, repo.ID, pagination.Options{})
		require.NoError(t, err)
		require.Equal(t, 2, len(contributors.Result))

		require.Equal(t, suite.users.Krosh.ID, contributors.Result[0].ID)
		require.Equal(t, suite.users.Barash.ID, contributors.Result[1].ID)
	})
}
