package integrationtests

import (
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcPRCommentReactions_Update() {
	t := suite.T()
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	likeID := grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))

	pr := suite.makePullRequest(suite.users.Admin, nil)
	comment := suite.makePrCommentGRPC(t, suite.users.Admin, pr, &makePrCommentOptions{Body: "Test PR comment for reactions"})
	commentID := grpc2.MarshalID(comment.ID)
	prID := grpc2.MarshalID(pr.ID)

	// Admin adds a reaction
	updatedComment, err := suite.addPRCommentReaction(adminCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists := updatedComment.Reactions[likeID]
	require.True(t, exists, "reaction must exist after admin adds")
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// Admin removes the reaction
	updatedComment, err = suite.removePRCommentReaction(adminCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}

	// Admin adds the reaction again
	updatedComment, err = suite.addPRCommentReaction(adminCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// Admin attempts to add a duplicate reaction (should return an error)
	_, err = suite.addPRCommentReaction(adminCtx, prID, commentID, likeID)
	require.ErrorContains(t, err, "already exists")

	// Verify via Get
	commentResp, err := pb.NewPRCommentServiceClient(suite.grpcClient).Get(adminCtx, &pb.GetCommentRequest{
		PrId:      prID,
		CommentId: commentID,
	})
	require.NoError(t, err)
	reaction, exists = commentResp.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)

	// User adds a reaction
	updatedComment, err = suite.addPRCommentReaction(kroshCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(2), reaction.Count)
	require.True(t, reaction.SelfReact)

	// User removes the reaction
	updatedComment, err = suite.removePRCommentReaction(kroshCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.False(t, reaction.SelfReact)

	// User attempts to remove the reaction again (should return an error)
	_, err = suite.removePRCommentReaction(kroshCtx, prID, commentID, likeID)
	require.ErrorContains(t, err, "not found")

	// Admin removes the reaction, count becomes zero
	updatedComment, err = suite.removePRCommentReaction(adminCtx, prID, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}
}

func (suite *RwApiTestSuite) addPRCommentReaction(ctx context.Context, prID, commentID, reactionID string) (*pb.PRComment, error) {
	req := &pb.UpdateCommentReactionRequest{
		PrId:      prID,
		CommentId: commentID,
		ReactionDelta: &pb.IdDelta{
			Id:     reactionID,
			Action: pb.DeltaAction_ADD,
		},
	}
	op, err := pb.NewPRCommentServiceClient(suite.grpcClient).UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(op, &pb.PRComment{})
}

func (suite *RwApiTestSuite) removePRCommentReaction(ctx context.Context, prID, commentID, reactionID string) (*pb.PRComment, error) {
	req := &pb.UpdateCommentReactionRequest{
		PrId:      prID,
		CommentId: commentID,
		ReactionDelta: &pb.IdDelta{
			Id:     reactionID,
			Action: pb.DeltaAction_REMOVE,
		},
	}
	op, err := pb.NewPRCommentServiceClient(suite.grpcClient).UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(op, &pb.PRComment{})
}

func (suite *RwApiTestSuite) TestGrpcPRCommentReactions_ListReactedUsers() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	likeID := grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))
	admin, krosh, barash := suite.users.Admin, suite.users.Krosh, suite.users.Barash
	adminCtx := testutils.AuthorizeGRPC(admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(krosh.Identity)

	// Create two pr comments
	pr := suite.makePullRequest(suite.users.Admin, nil)
	comment1 := suite.makePrCommentGRPC(t, suite.users.Admin, pr, &makePrCommentOptions{Body: "PR comment for reactions test #1"})
	comment2 := suite.makePrCommentGRPC(t, suite.users.Admin, pr, &makePrCommentOptions{Body: "PR comment for reactions test #2", NeedResolution: true})
	comment1ID := grpc2.MarshalID(comment1.ID)
	comment2ID := grpc2.MarshalID(comment2.ID)
	prID := grpc2.MarshalID(pr.ID)

	t.Run("All reacted on first PR comment", func(t *testing.T) {
		// Add reaction from admin, krosh and barash
		for _, user := range []*entities.User{admin, krosh, barash} {
			userCtx := testutils.AuthorizeGRPC(user.Identity)
			_, err := suite.addPRCommentReaction(userCtx, prID, comment1ID, likeID)
			require.NoError(t, err)
		}

		resp, err := client.ListReactedUsers(adminCtx, &pb.ListCommentReactedUsersRequest{
			PrId:       prID,
			CommentId:  comment1ID,
			ReactionId: likeID,
		})
		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})

	t.Run("All reacted on second PR comment", func(t *testing.T) {
		// Add reaction from admin and krosh
		for _, user := range []*entities.User{admin, krosh} {
			userCtx := testutils.AuthorizeGRPC(user.Identity)
			_, err := suite.addPRCommentReaction(userCtx, prID, comment2ID, likeID)
			require.NoError(t, err)
		}

		resp, err := client.ListReactedUsers(kroshCtx, &pb.ListCommentReactedUsersRequest{
			PrId:       prID,
			CommentId:  comment2ID,
			ReactionId: likeID,
		})
		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})
}
