package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPRRefresh_DiscardedToOpen() {
	t := suite.T()

	// Create new repo
	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repo, err := suite.RepoRepo.GetRepository(context.Background(), orgSlug, repoSlug)
	require.NoError(t, err)

	// Create main and feature branches
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesMaintainer)

	cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
	cg.Must(t, "checkout", "-b", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "init"), "content")
	cg.Must(t, "push", "--set-upstream", "origin", "main")
	cg.Must(t, "checkout", "-b", "feature")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "file"), "content")
	cg.Must(t, "push", "--set-upstream", "origin", "feature")

	// Create PR from feature to main
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	op, err := client.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:  grpc_marshalling.IDInverse(repo.ID),
		Title:   "PR",
		Source:  "feature",
		Target:  "main",
		Publish: true,
	})
	require.NoError(t, err)
	pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.CalculateMergeConflict)

	checks, err := client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)

	require.Equal(t, 0, len(checks.MergeChecks.Conflicts.Conflicts))

	// Discard PR
	_, err = client.Discard(ctx, &pb.DiscardRequest{
		Id: pr.Id,
	})
	require.NoError(t, err)

	// Push to main
	cg.Must(t, "checkout", "main")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "conflict_file"), "content")
	cg.Must(t, "push", "--set-upstream", "origin", "main")

	// Push to feature
	cg.Must(t, "checkout", "feature")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "conflict_file"), "new content")
	cg.Must(t, "push", "--set-upstream", "origin", "feature")

	// Check PR is not changed
	_, err = client.Get(ctx, &pb.GetPullRequestRequest{
		Identity: &pb.GetPullRequestRequest_Id{
			Id: pr.Id,
		},
	})
	require.NoError(t, err)
	changedPr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)
	require.Equal(t, pr.Iteration, changedPr.Iteration)
	checks, err = client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)
	require.Equal(t, 0, len(checks.MergeChecks.Conflicts.Conflicts))

	// Reopen PR
	op, err = client.Reopen(ctx, &pb.ReopenRequest{
		Id: pr.Id,
	})
	require.NoError(t, err)
	reopenedPr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)

	// Check PR is refreshed
	require.NotEqual(t, pr.Iteration, reopenedPr.Iteration)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.CalculateMergeConflict)

	checks, err = client.ListMergeChecks(ctx, &pb.ListMergeChecksRequest{
		PrId: pr.Id,
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(checks.MergeChecks.Conflicts.Conflicts))
}
