package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestMerge() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Description: "some description\nmultiline",
	})
	prEntity, err := suite.PullRequestService.Get(context.Background(), pr.ID)
	require.NoError(t, err)

	t.Run("contributor can't merge", func(t *testing.T) {
		suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
		ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
		_, err = client.Merge(ctx, &pb.MergeRequest{
			PrId: grpc.MarshalID(prEntity.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("maintainer can merge", func(t *testing.T) {
		suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
		suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
		require.NoError(t, suite.PullRequestService.SetDecision(
			context.Background(),
			testutils.NewStubAuthenticator(&suite.users.Krosh.Identity),
			prEntity,
			suite.users.Krosh,
			&entities.PullRequestDecisions.Ship,
			entities.NotifyOptions{},
		))

		err := suite.PullRequestService.WaitForMergeConflictCalculation(context.Background(), prEntity.ID)
		require.NoError(t, err)

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		mergeResp, err := client.Merge(ctx, &pb.MergeRequest{
			PrId: grpc.MarshalID(pr.ID),
			MergeParameters: &pb.MergeParameters{
				Squash: utils.PtrFromValue(true),
			},
			NotificationOptions: &pb.NotificationOptions{},
			Force:               false,
		})
		require.NoError(t, err)

		fr := mergeResp.ProtoReflect()
		fr.Range(func(descriptor protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			return false
		})

		require.Equal(t, "merge", mergeResp.Description)
		require.Equal(t, false, mergeResp.Done)

		meta, err := mergeResp.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.OperationMetadata{
			Object: &pb.ObjectIdentity{
				Type: pb.ObjectIdentity_REPOSITORY,
				Id:   grpc.MarshalID(prEntity.RepoID),
			},
			Status: pb.OperationMetadata_SCHEDULED,
		}, meta)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

		prEntity, err := suite.PullRequestService.Get(context.Background(), pr.ID)
		require.NoError(t, err)
		require.Equal(t, prEntity.Status, entities.PRStatuses.Merged)
		require.NotEmpty(t, prEntity.Merge.MergeCommitSHA)

		repoServiceClient := pb.NewRepoServiceClient(suite.grpcClient)
		commits, err := repoServiceClient.ListCommits(ctx, &pb.ListCommitsRequest{
			Id:       grpc.MarshalID(prEntity.RepoID),
			PageSize: utils.PtrFromValue(uint64(1)),
		})
		require.NoError(t, err)

		require.Equal(t, 1, len(commits.Commits))

		// yarequire.ProtoDumpFixture(t, commits.Commits[0])
		yarequire.ProtoCompareWithFixture(t, commits.Commits[0],
			protocmp.IgnoreFields(&pb.Commit{}, "hash", "parent_commits"),
			protocmp.IgnoreFields(&pb.Signature{}, "date"),
		)
	})
}

func (suite *RwApiTestSuite) TestForceMerge() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)
	pr := suite.makePullRequest(suite.users.Kopatych, nil)
	prEntity, err := suite.PullRequestService.Get(context.Background(), pr.ID)
	require.NoError(t, err)

	t.Run("contributor can't force merge", func(t *testing.T) {
		suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

		ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
		_, err = client.Merge(ctx, &pb.MergeRequest{
			PrId:  grpc.MarshalID(prEntity.ID),
			Force: true,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
	t.Run("PR owner can't force merge", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		_, err = client.Merge(ctx, &pb.MergeRequest{
			PrId:  grpc.MarshalID(prEntity.ID),
			Force: true,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("maintainer can force merge", func(t *testing.T) {
		suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		merge, err := client.Merge(ctx, &pb.MergeRequest{
			PrId:  grpc.MarshalID(prEntity.ID),
			Force: true,
		})
		require.NoError(t, err)

		require.Equal(t, "merge", merge.Description)
		require.Equal(t, false, merge.Done)

		meta, err := merge.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.OperationMetadata{
			Object: &pb.ObjectIdentity{
				Type: pb.ObjectIdentity_REPOSITORY,
				Id:   grpc.MarshalID(prEntity.RepoID),
			},
			Status: pb.OperationMetadata_SCHEDULED,
		}, meta)
	})
}

func (suite *RwApiTestSuite) TestMerge_Blocked() {
	t := suite.T()

	repo := suite.repos.PrValidation
	suite.addRole(t, suite.users.Krosh, repo, iam.Roles.RepositoriesMaintainer)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)

	suite.mustBash(repo, `
		git checkout -b conflict-1
		echo "something" > conflict.txt
		git add .
		git commit -m "First conflict"
	`)
	suite.mustBash(repo, `
		git checkout -b conflict-2
		echo "anything else" > conflict.txt
		git add .
		git commit -m "Second conflict"
	`)

	client := pb.NewPRServiceClient(suite.grpcClient)
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "conflict-2",
		Target: "conflict-1",
	})

	prEntity, err := suite.PullRequestService.Get(context.Background(), pr.ID)
	require.NoError(t, err)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	t.Run("merge blocked", func(t *testing.T) {
		_, err := client.Merge(ctx, &pb.MergeRequest{
			PrId: grpc.MarshalID(prEntity.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("force merge blocked", func(t *testing.T) {
		_, err := client.Merge(ctx, &pb.MergeRequest{
			PrId:  grpc.MarshalID(prEntity.ID),
			Force: true,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

		prEntity, err := suite.PullRequestService.Get(context.Background(), pr.ID)
		require.NoError(t, err)
		require.Equal(t, prEntity.Status, entities.PRStatuses.Open)
		require.NotEmpty(t, prEntity.Merge.Error)
	})
}
