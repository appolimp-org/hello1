package integrationtests

import (
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestPRGetChanges() {
	t := suite.T()
	ctx := context.Background()

	suite.addRole(t, suite.users.Kopatych, suite.repos.TreeDiff, iam.Roles.RepositoriesContributor)
	grpcCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	client := pb.NewPRServiceClient(suite.grpcClient)

	// Create PR for basic tests (main → branch)
	prOp, err := client.Create(grpcCtx, &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.TreeDiff.ID),
		Title:               "title",
		Source:              "branch",
		Target:              "main",
		Publish:             true,
		NotificationOptions: testutils.SkipNotificationPb,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	require.NoError(t, err)
	pr := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, prOp)

	// Create PR for rename test (range_base → range_iter1)
	renamePROp, err := client.Create(grpcCtx, &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.TreeDiff.ID),
		Title:               "title",
		Source:              "rename",
		Target:              "rename_before",
		Publish:             true,
		NotificationOptions: testutils.SkipNotificationPb,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	require.NoError(t, err)
	renamePR := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, renamePROp)

	// Create PR for iteration diff test (range_base → range_iter1)
	rangePROp, err := client.Create(grpcCtx, &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.TreeDiff.ID),
		Title:               "title",
		Source:              "range_iter1",
		Target:              "range_base",
		Publish:             true,
		NotificationOptions: testutils.SkipNotificationPb,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	require.NoError(t, err)
	rangePR := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, rangePROp)

	prRepo := suite.PullRequestRepoFactory.Build(suite.repos.TreeDiff.ID)
	rangePRID, err := grpc_marshalling.IDDirect(rangePR.Id)
	require.NoError(t, err)

	newIterID, err := prRepo.CreateIteration(ctx, &entities.PullRequestIteration{
		PullRequestID: rangePRID,
		CommitHash:    plumbing.NewHash("ab2449eb600f73d723a8cde24f13b96ae3264a0e"),
		MergebaseHash: plumbing.NewHash("e59af2c289a179b40d118a7a202c1f7901990979"),
	})
	require.NoError(t, err)

	// Create PR for multiple merge base test
	basePROp, err := client.Create(grpcCtx, &pb.CreatePullRequestRequest{
		RepoId:              grpc2.MarshalID(suite.repos.Crisscross.ID),
		Title:               "title",
		Source:              "G",
		Target:              "F",
		Publish:             true,
		NotificationOptions: testutils.SkipNotificationPb,
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	require.NoError(t, err)
	basePR := testutils.UnmarshalGrpcResult[*pb.PullRequest](t, basePROp)

	testcases := []struct {
		name    string
		request *pb.GetChangesRequest
		expCode codes.Code
	}{
		{
			name: "default",
			request: &pb.GetChangesRequest{
				PrId: pr.Id,
			},
		},
		{
			name: "same iteration",
			request: &pb.GetChangesRequest{
				PrId:       pr.Id,
				FromIterId: utils.PtrFromValue(pr.Iteration),
				IterId:     utils.PtrFromValue(pr.Iteration),
			},
		},
		{
			name: "iterations diff",
			request: &pb.GetChangesRequest{
				PrId:       rangePR.Id,
				FromIterId: utils.PtrFromValue(rangePR.Iteration),
				IterId:     utils.PtrFromValue(grpc_marshalling.IDInverse(newIterID)),
			},
		},
		{
			name: "rename",
			request: &pb.GetChangesRequest{
				PrId: renamePR.Id,
			},
		},
		{
			name: "multiple_merge_bases",
			request: &pb.GetChangesRequest{
				PrId: basePR.Id,
			},
		},
		{
			name: "not found",
			request: &pb.GetChangesRequest{
				PrId:   pr.Id,
				IterId: utils.PtrFromValue("111111"),
			},
			expCode: codes.NotFound,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {

			resp, err := client.GetChanges(grpcCtx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expCode, err)
			if tc.expCode != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
