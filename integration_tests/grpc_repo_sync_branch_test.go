package integrationtests

import (
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestGrpcSyncBranches() {
	t := suite.T()

	repoOwner := suite.users.Krosh

	ctx := testutils.AuthorizeGRPC(repoOwner.Identity)

	repo, _ := suite.createRepo(t, repoOwner, "test-repo", pb.ResourceVisibility_RESOURCE_PUBLIC, "")

	origCloneURL := suite.HTTPSProtocol().RepoURL(repo.OrgSlug, repo.Slug)

	fork := suite.forkRepo(t, repoOwner, repo.Id)

	suite.addCommits(t, origCloneURL, repoOwner, commitsOptions{id: "orig"})

	client := pb.NewRepoServiceClient(suite.grpcClient)

	_, err := client.SyncBranch(ctx, &pb.SyncBranchRequest{
		ForkBranch: &pb.BranchPointer{
			RepoId:     fork.Id,
			BranchName: "master",
		},
		ParentBranch: &pb.BranchPointer{
			RepoId:     repo.Id,
			BranchName: "master",
		},
		SyncType: pb.SyncBranchRequest_FAST_FORWARD,
	})
	require.NoError(t, err)

	masterParent, err := client.ResolveRevision(ctx, &pb.ResolveRevisionRequest{
		Id:       repo.Id,
		Revision: "master",
	})
	require.NoError(t, err)

	masterFork, err := client.ResolveRevision(ctx, &pb.ResolveRevisionRequest{
		Id:       fork.Id,
		Revision: "master",
	})
	require.NoError(t, err)

	require.Equal(t, masterFork.Commit.Hash, masterParent.Commit.Hash)
}
