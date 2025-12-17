package integrationtests

import (
	"common/testutils/assertjson"
	"context"
	"fmt"
	"gitcore/internal/interfaces"
	"google.golang.org/protobuf/encoding/protojson"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"common/cgit"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) validateLastFeedItem(t *testing.T, pr *entities.PullRequest, expectedJSON string) uint64 {
	suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)

	feed, err := pb.NewPRServiceClient(suite.grpcClient).ListFeed(
		testutils.AuthorizeGRPC(suite.users.Admin.Identity),
		&pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
	require.NoError(t, err)

	require.NotEmpty(t, feed.Events)
	data, err := protojson.Marshal(feed.Events[0])
	require.NoError(t, err)
	//print(utils.PrettyPrint(feed.Events))

	assertjson.Match(t, data, expectedJSON)
	id, err := grpc.ParseID(feed.Events[0].Id)
	require.NoError(t, err)
	return id
}

func (suite *RwApiTestSuite) TestPrFeed_MigratedUsers() {
	t := suite.T()

	// create migrated user
	migratedUser, err := suite.UserRepo.CreateUser(context.Background(), entities.User{
		Username: "123@github",
		Email:    "123@github",
		Identity: entities.UserIdentity{
			ID:  "123",
			Src: entities.IdentityProviders.Migration,
		},
		Visibility:  entities.Visibilities.Public,
		DisplayName: "123",
		Status:      entities.UserStatuses.Active,
	})
	require.NoError(suite.T(), err)

	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	// migrated pr feed
	_, err = suite.PullRequestFeedRepo.OnReviewDecision(context.Background(), pr.ID, migratedUser.ID, &entities.PullRequestDecisions.Ship)
	require.NoError(suite.T(), err)

	// real user got into feed
	_, err = suite.PullRequestFeedRepo.OnReviewDecision(context.Background(), pr.ID, suite.users.Kopatych.ID, &entities.PullRequestDecisions.Ship)
	require.NoError(suite.T(), err)

	feed, err := pb.NewPRServiceClient(suite.grpcClient).ListFeed(
		testutils.AuthorizeGRPC(suite.users.Admin.Identity),
		&pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
	require.NoError(t, err)
	require.Len(t, feed.Events, 2)

	t.Run("kopatych", func(t *testing.T) {
		data, err := protojson.Marshal(feed.Events[0])
		require.NoError(t, err)
		assertjson.Match(t, data, fmt.Sprintf(`{
		  "eventType": "REVIEW_DECISION",
		  "details": {
		    "reviewDecision": {
			  "decision": "RD_APPROVE",
			  "userId": "%d"
		    }
		  }
		}`, suite.users.Kopatych.ID))
	})

	t.Run("migrated user", func(t *testing.T) {
		data, err := protojson.Marshal(feed.Events[1])
		require.NoError(t, err)
		assertjson.Match(t, data, fmt.Sprintf(`{
		  "eventType": "REVIEW_DECISION",
		  "details": {
		    "reviewDecision": {
			  "decision": "RD_APPROVE",
			  "userId": "%d"
		    }
		  }
		}`, migratedUser.ID))
	})
}

func (suite *RwApiTestSuite) TestGrpcPrFeed() {
	pr := suite.makePullRequest(suite.users.Barash, nil)
	ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	var comment1 *entities.PullRequestComment
	var comment2 *entities.PullRequestComment
	var comment3 *entities.PullRequestComment

	suite.T().Run("pr updated", func(t *testing.T) {
		suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
		_, err := client.Update(ctx, &pb.UpdatePullRequestRequest{
			Id:          grpc.MarshalID(pr.ID),
			Title:       utils.PtrFromValue("My new title"),
			Description: utils.PtrFromValue("AZAZA"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "description"},
			},
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
			Parents: &pb.RepositoryFullSlug{
				OrgSlug:  "yandex",
				RepoSlug: "alpha",
			},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0], protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"))
	})

	suite.T().Run("new iteration", func(t *testing.T) {
		ctx := context.Background()

		suite.addRole(suite.T(), suite.users.Raichu, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

		tmpDir := testutils.TempDir(t, "", "pr_concurrent")
		cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Raichu))
		cg.Must(t, "clone", suite.URL(suite.repos.Alpha))
		cg = cgit.NewCGit(path.Join(tmpDir, "alpha")).
			WithAuthToken(testutils.FakeIAMAuthToken(testutils.UserIdentities.Raichu))

		cg.Must(t, "checkout", "branch")
		suite.makeNewFile(cg, "a")
		cg.Must(t, "add", "a")
		cg.Must(t, "commit", "-m", "\"commit\"")
		cg.Must(t, "push")

		authenticator := suite.getFakeAuthenticator(suite.users.Raichu.Identity)
		headCommit, err := suite.GitcoreClient.GetCommit(ctx, authenticator, interfaces.GetCommitArgs{
			Revision: entities.NewGitRevisionFromRev(suite.repos.Alpha.ID, utils.PtrFromValue("branch")),
		})
		require.NoError(t, err)
		hash := headCommit.Hash

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 2, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
			protocmp.IgnoreFields(&pb.PullRequestFeedItem_IterationDetails{}, "iteration"),
		)
		require.Equal(t, hash.String(), resp.Events[0].Details.Details.(*pb.PullRequestFeedItem_Details_Iteration).Iteration.Iteration.CommitHash)
	})

	suite.T().Run("create comments", func(t *testing.T) {
		comment1 = suite.makePrCommentGRPC(t, suite.users.Barash, pr, &makePrCommentOptions{Body: "A"})
		comment2 = suite.makePrCommentGRPC(t, suite.users.Barash, pr, &makePrCommentOptions{Body: "B", Draft: true, NeedResolution: true})
		comment3 = suite.makePrCommentGRPC(t, suite.users.Barash, pr, &makePrCommentOptions{Body: "C", Draft: true, NeedResolution: true})

		suite.makePrCommentGRPC(t, suite.users.Barash, pr, &makePrCommentOptions{Body: "subA", ParentID: &comment1.ID})
		suite.makePrCommentGRPC(t, suite.users.Barash, pr, &makePrCommentOptions{Body: "subB", NeedResolution: true, ParentID: &comment2.ID})

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 3, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
			protocmp.IgnoreFields(&pb.PullRequestFeedItem_CommentDetails{}, "comment_id"),
		)
		require.Equal(t, grpc.MarshalID(comment1.ID), resp.Events[0].Details.Details.(*pb.PullRequestFeedItem_Details_Comment).Comment.CommentId)
	})

	suite.T().Run("publish comments", func(t *testing.T) {
		commentsClient := pb.NewPRCommentServiceClient(suite.grpcClient)
		_, err := commentsClient.PublishDrafts(ctx, &pb.PublishDraftsRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 5, len(resp.Events))
		resp.Events = resp.Events[:2]
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
			protocmp.IgnoreFields(&pb.PullRequestFeedItem_CommentDetails{}, "comment_id"),
		)
		require.Equal(t, grpc.MarshalID(comment3.ID), resp.Events[0].Details.Details.(*pb.PullRequestFeedItem_Details_Comment).Comment.CommentId)
		require.Equal(t, grpc.MarshalID(comment2.ID), resp.Events[1].Details.Details.(*pb.PullRequestFeedItem_Details_Comment).Comment.CommentId)
	})

	suite.T().Run("assign reviewer", func(t *testing.T) {
		reviewClient := pb.NewPRReviewersServiceClient(suite.grpcClient)
		_, err := reviewClient.Set(ctx, &pb.SetReviewersRequest{
			PrId:    grpc.MarshalID(pr.ID),
			UserIds: []string{grpc.MarshalID(suite.users.Krosh.ID)},
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 6, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		)
	})

	suite.T().Run("assign and remove reviewer", func(t *testing.T) {
		reviewClient := pb.NewPRReviewersServiceClient(suite.grpcClient)
		_, err := reviewClient.Set(ctx, &pb.SetReviewersRequest{
			PrId:    grpc.MarshalID(pr.ID),
			UserIds: []string{grpc.MarshalID(suite.users.Pikachu.ID)},
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 7, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		)
	})

	suite.T().Run("set review decision", func(t *testing.T) {
		reviewClient := pb.NewPRReviewersServiceClient(suite.grpcClient)
		_, err := reviewClient.SetDecision(ctx, &pb.SetDecisionRequest{
			PrId:           grpc.MarshalID(pr.ID),
			ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 8, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		)
	})

	suite.T().Run("remove review decision", func(t *testing.T) {
		reviewClient := pb.NewPRReviewersServiceClient(suite.grpcClient)
		_, err := reviewClient.SetDecision(ctx, &pb.SetDecisionRequest{
			PrId:           grpc.MarshalID(pr.ID),
			ReviewDecision: nil,
		})
		require.NoError(t, err)

		resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
			PrId: grpc.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Equal(t, 9, len(resp.Events))
		yarequire.ProtoCompareWithFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		)
	})

	suite.T().Run("pagination", func(t *testing.T) {
		pageToken := ""
		var res []*pb.PullRequestFeedItem
		for {
			req := &pb.ListPullRequestFeedRequest{
				PrId:     grpc.MarshalID(pr.ID),
				PageSize: utils.PtrFromValue(uint64(1)),
			}
			if pageToken != "" {
				req.PageToken = &pageToken
			}
			resp, err := client.ListFeed(ctx, req)
			require.NoError(t, err)
			res = append(res, resp.Events...)
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		require.Equal(t, 9, len(res))
	})
}

func (suite *RwApiTestSuite) TestGrpcPrFeed_MergeSuccess() {
	t := suite.T()

	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
	pr := suite.makePullRequest(suite.users.Barash, nil)

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)
	_, err := client.Merge(ctx, &pb.MergeRequest{
		PrId: grpc.MarshalID(pr.ID), Force: true,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	authenticator := suite.getFakeAuthenticator(suite.users.Barash.Identity)
	headCommit, err := suite.GitcoreClient.GetCommit(ctx, authenticator, interfaces.GetCommitArgs{
		Revision: entities.NewGitRevisionFromRev(suite.repos.Alpha.ID, utils.PtrFromValue("master")),
	})
	require.NoError(t, err)
	hash := headCommit.Hash

	resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
		PrId: grpc.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	yarequire.ProtoCompareWithFixture(t, resp.Events[0],
		protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		protocmp.IgnoreFields(&pb.PullRequestFeedItem_MergeSuccessDetails{}, "merge_commit_hash"),
	)
	require.Equal(t, hash.String(), resp.Events[0].Details.Details.(*pb.PullRequestFeedItem_Details_MergeSuccess).MergeSuccess.MergeCommitHash)
}

func (suite *RwApiTestSuite) TestGrpcPrFeed_MergeFailure() {
	t := suite.T()

	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)

	cg, tmpDir := suite.initCGit(suite.users.Barash, suite.repos.Alpha.FullSlug())
	cg.Must(t, "checkout", "-b", "branch-2")
	commit(t, &cg, path.Join(tmpDir, suite.repos.Alpha.Slug, "tmp.file"), "content2")
	cg.Must(t, "push", "origin", "branch-2")
	cg.Must(t, "checkout", "master")
	commit(t, &cg, path.Join(tmpDir, suite.repos.Alpha.Slug, "tmp.file"), "content")
	cg.Must(t, "push", "origin", "master")

	pr := suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Source: "branch-2",
		Target: "master",
	})

	client := pb.NewPRServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)
	_, err := client.Merge(ctx, &pb.MergeRequest{
		PrId: grpc.MarshalID(pr.ID), Force: true,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)

	resp, err := client.ListFeed(ctx, &pb.ListPullRequestFeedRequest{
		PrId: grpc.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	/*
		yarequire.ProtoDumpFixture(t, resp.Events[0],
			protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
		)
	*/
	yarequire.ProtoCompareWithFixture(t, resp.Events[0],
		protocmp.IgnoreFields(&pb.PullRequestFeedItem{}, "created_at", "id"),
	)
}
