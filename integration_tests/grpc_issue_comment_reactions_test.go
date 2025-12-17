package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcIssueCommentReactions_Update() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	likeID := entities.Reactions.Like.String()

	comment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		Body: "Test comment for reactions",
	})
	commentID := grpc_marshalling.IDInverse(comment.ID)

	// admin adds reaction
	updatedComment, err := suite.addIssueCommentReaction(adminCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists := updatedComment.Reactions[likeID]
	require.True(t, exists, "reaction must exist after admin adds")
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// admin removes reaction
	updatedComment, err = suite.removeIssueCommentReaction(adminCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}

	// admin adds reaction again
	updatedComment, err = suite.addIssueCommentReaction(adminCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// admin tries to add duplicate reaction (should error)
	_, err = suite.addIssueCommentReaction(adminCtx, commentID, likeID)
	require.ErrorContains(t, err, "already exists")

	// verify reaction count via Get
	commentResp, err := client.Get(adminCtx, &pb.GetIssueCommentRequest{
		CommentId: commentID,
	})
	require.NoError(t, err)
	reaction, exists = commentResp.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)

	// user adds reaction
	updatedComment, err = suite.addIssueCommentReaction(kroshCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(2), reaction.Count)
	require.True(t, reaction.SelfReact)

	// user removes reaction
	updatedComment, err = suite.removeIssueCommentReaction(kroshCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.False(t, reaction.SelfReact)

	// user tries to remove reaction again (should error)
	_, err = suite.removeIssueCommentReaction(kroshCtx, commentID, likeID)
	require.ErrorContains(t, err, "not found")

	// admin removes reaction, reaction count becomes zero
	updatedComment, err = suite.removeIssueCommentReaction(adminCtx, commentID, likeID)
	require.NoError(t, err)
	reaction, exists = updatedComment.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}
}

func (suite *RwApiTestSuite) addIssueCommentReaction(ctx context.Context, commentID string, reactionID string) (*pb.IssueComment, error) {
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	req := &pb.UpdateIssueCommentReactionRequest{
		CommentId: commentID,
		ReactionDelta: &pb.IdDelta{
			Id:     reactionID,
			Action: pb.DeltaAction_ADD,
		},
	}
	resp, err := client.UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(resp, &pb.IssueComment{})
}

func (suite *RwApiTestSuite) removeIssueCommentReaction(ctx context.Context, commentID string, reactionID string) (*pb.IssueComment, error) {
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	req := &pb.UpdateIssueCommentReactionRequest{
		CommentId: commentID,
		ReactionDelta: &pb.IdDelta{
			Id:     reactionID,
			Action: pb.DeltaAction_REMOVE,
		},
	}
	resp, err := client.UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(resp, &pb.IssueComment{})
}

func (suite *RwApiTestSuite) TestGrpcIssueCommentReactions_ListReactedUsers() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	likeID := grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))
	admin, krosh, barash := suite.users.Admin, suite.users.Krosh, suite.users.Barash
	adminCtx := testutils.AuthorizeGRPC(admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(krosh.Identity)

	// Create two public comments
	comment1 := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		Body: "Comment for reactions test #1",
	})
	comment1ID := grpc_marshalling.IDInverse(comment1.ID)

	comment2 := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		Body: "Comment for reactions test #2",
	})
	comment2ID := grpc_marshalling.IDInverse(comment2.ID)

	t.Run("All reacted on first public comment", func(t *testing.T) {
		req := &pb.UpdateIssueCommentReactionRequest{
			CommentId: comment1ID,
			ReactionDelta: &pb.IdDelta{
				Id:     likeID,
				Action: pb.DeltaAction_ADD,
			},
		}

		// Add reaction from admin, krosh and barash
		for _, user := range []*entities.User{admin, krosh, barash} {
			userCtx := testutils.AuthorizeGRPC(user.Identity)
			_, err := client.UpdateReactions(userCtx, req)
			require.NoError(t, err)
		}

		resp, err := client.ListReactedUsers(kroshCtx, &pb.ListReactedUsersRequest{
			Id:         comment1ID,
			ReactionId: likeID,
		})
		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})

	t.Run("All reacted on second public comment", func(t *testing.T) {
		req := &pb.UpdateIssueCommentReactionRequest{
			CommentId: comment2ID,
			ReactionDelta: &pb.IdDelta{
				Id:     likeID,
				Action: pb.DeltaAction_ADD,
			},
		}

		// Add reaction from admin and krosh
		for _, user := range []*entities.User{admin, krosh} {
			userCtx := testutils.AuthorizeGRPC(user.Identity)
			_, err := client.UpdateReactions(userCtx, req)
			require.NoError(t, err)
		}

		resp, err := client.ListReactedUsers(adminCtx, &pb.ListReactedUsersRequest{
			Id:         comment2ID,
			ReactionId: likeID,
		})
		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})
}
