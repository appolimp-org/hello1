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
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

var likeReactionID = grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))

func (suite *RwApiTestSuite) TestGrpcIssueReactions_Update() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	likeID := grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue for reactions test",
		Visibility: entities.IssueVisibilities.Public,
	})
	issueID := grpc_marshalling.IDInverse(issue.ID)

	// admin add reaction
	updatedIssue, err := suite.addReaction(adminCtx, issueID)
	require.NoError(t, err)
	reaction, exists := updatedIssue.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), issue.Votes)

	// admin remove reaction
	updatedIssue, err = suite.removeReaction(adminCtx, issueID)
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(0), issue.Votes)

	// admin add reaction
	updatedIssue, err = suite.addReaction(adminCtx, issueID)
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.True(t, reaction.SelfReact)

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), issue.Votes)

	// admin try add another reaction
	_, err = suite.addReaction(adminCtx, issueID)
	require.ErrorContains(t, err, "already exists")

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), issue.Votes)

	// krosh check reaction
	updatedIssue, err = client.Get(kroshCtx, &pb.GetIssueRequest{Issue: &pb.GetIssueRequest_Id{Id: issueID}})
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.False(t, reaction.SelfReact)

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), issue.Votes)

	// krosh add reaction
	updatedIssue, err = suite.addReaction(kroshCtx, issueID)
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(2), reaction.Count)
	require.True(t, reaction.SelfReact)

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(2), issue.Votes)

	// krosh rm reaction
	updatedIssue, err = suite.removeReaction(kroshCtx, issueID)
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	require.True(t, exists)
	require.Equal(t, int32(1), reaction.Count)
	require.False(t, reaction.SelfReact)

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), issue.Votes)

	// krosh try to rm reaction again
	_, err = suite.removeReaction(kroshCtx, issueID)
	require.ErrorContains(t, err, "not found")

	// admin rm reaction
	updatedIssue, err = suite.removeReaction(adminCtx, issueID)
	require.NoError(t, err)
	reaction, exists = updatedIssue.Reactions[likeID]
	if exists {
		require.Equal(t, int32(0), reaction.Count)
		require.False(t, reaction.SelfReact)
	}

	// check issue votes
	issue, err = suite.IssueService.Get(adminCtx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, int32(0), issue.Votes)
}

func (suite *RwApiTestSuite) TestGrpcIssueReactions_ListIssues() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue with reactions 1",
		Visibility: entities.IssueVisibilities.Public,
	})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue with reactions 2",
		Visibility: entities.IssueVisibilities.Public,
	})
	issue3 := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Other Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	addReaction := func(ctx context.Context, issueID string) *pb.Issue {
		req := &pb.UpdateIssueReactionRequest{
			Id: issueID,
			ReactionDelta: &pb.IdDelta{
				Id:     likeReactionID,
				Action: pb.DeltaAction_ADD,
			},
		}
		resp, err := client.UpdateReactions(ctx, req)
		require.NoError(t, err)
		updatedIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
		require.NoError(t, err)
		return updatedIssue
	}

	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	// add reactions
	addReaction(adminCtx, grpc_marshalling.IDInverse(issue1.ID))
	addReaction(kroshCtx, grpc_marshalling.IDInverse(issue1.ID))
	addReaction(kroshCtx, grpc_marshalling.IDInverse(issue2.ID))
	addReaction(adminCtx, grpc_marshalling.IDInverse(issue3.ID))

	tt := map[string]struct {
		ctx            context.Context
		expectedIssues map[string]struct {
			reactionCount int32
			selfReact     bool
		}
	}{
		"list by admin": {
			ctx: adminCtx,
			expectedIssues: map[string]struct {
				reactionCount int32
				selfReact     bool
			}{
				grpc_marshalling.IDInverse(issue1.ID): {reactionCount: 2, selfReact: true},
				grpc_marshalling.IDInverse(issue2.ID): {reactionCount: 1, selfReact: false},
				grpc_marshalling.IDInverse(issue3.ID): {reactionCount: 1, selfReact: true},
			},
		},
		"list by krosh": {
			ctx: kroshCtx,
			expectedIssues: map[string]struct {
				reactionCount int32
				selfReact     bool
			}{
				grpc_marshalling.IDInverse(issue1.ID): {reactionCount: 2, selfReact: true},
				grpc_marshalling.IDInverse(issue2.ID): {reactionCount: 1, selfReact: true},
				grpc_marshalling.IDInverse(issue3.ID): {reactionCount: 1, selfReact: false},
			},
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			listReq := &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			}
			listResp, err := client.List(tc.ctx, listReq)
			require.NoError(t, err)
			require.Len(t, listResp.Issues, len(tc.expectedIssues))
			for _, issue := range listResp.Issues {
				exp, ok := tc.expectedIssues[issue.Id]
				require.True(t, ok, "Unexpected issue ID: %s", issue.Id)
				reaction, exists := issue.Reactions[likeReactionID]
				require.True(t, exists)
				require.Equal(t, exp.reactionCount, reaction.Count)
				require.Equal(t, exp.selfReact, reaction.SelfReact)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcIssueReactions_ListUserIssues() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "User Issue with reactions 1",
		Visibility: entities.IssueVisibilities.Public,
	})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "User Issue with reactions 2",
		Visibility: entities.IssueVisibilities.Public,
	})
	issue3 := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Other User Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	userCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	// add reactions
	_, err := suite.addReaction(adminCtx, grpc_marshalling.IDInverse(issue1.ID))
	require.NoError(t, err)
	_, err = suite.addReaction(userCtx, grpc_marshalling.IDInverse(issue1.ID))
	require.NoError(t, err)
	_, err = suite.addReaction(userCtx, grpc_marshalling.IDInverse(issue2.ID))
	require.NoError(t, err)
	_, err = suite.addReaction(adminCtx, grpc_marshalling.IDInverse(issue3.ID))
	require.NoError(t, err)

	tt := map[string]struct {
		ctx            context.Context
		expectedIssues map[string]struct {
			reactionCount int32
			selfReact     bool
		}
	}{
		"list by admin": {
			ctx: adminCtx,
			expectedIssues: map[string]struct {
				reactionCount int32
				selfReact     bool
			}{
				grpc_marshalling.IDInverse(issue1.ID): {reactionCount: 2, selfReact: true},
				grpc_marshalling.IDInverse(issue2.ID): {reactionCount: 1, selfReact: false},
			},
		},
		"list by krosh": {
			ctx: userCtx,
			expectedIssues: map[string]struct {
				reactionCount int32
				selfReact     bool
			}{
				grpc_marshalling.IDInverse(issue3.ID): {reactionCount: 1, selfReact: false},
			},
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			listResp, err := client.ListUserIssues(tc.ctx, &pb.ListUserIssuesRequest{})
			require.NoError(t, err)
			require.Len(t, listResp.Issues, len(tc.expectedIssues))
			for _, issue := range listResp.Issues {
				exp, ok := tc.expectedIssues[issue.Id]
				require.True(t, ok, "Unexpected issue ID: %s", issue.Id)
				reaction, exists := issue.Reactions[likeReactionID]
				require.True(t, exists)
				require.Equal(t, exp.reactionCount, reaction.Count)
				require.Equal(t, exp.selfReact, reaction.SelfReact)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcIssueReactions_ListReactedUsers() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repo := suite.repos.Alpha
	likeID := grpc_marshalling.IDInverse(uint64(entities.Reactions.Like))
	admin, krosh, barash := suite.users.Admin, suite.users.Krosh, suite.users.Barash
	kroshCtx := testutils.AuthorizeGRPC(krosh.Identity)
	adminCtx := testutils.AuthorizeGRPC(admin.Identity)

	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repo.ID,
		Title:      "Issue for reactions test",
		Visibility: entities.IssueVisibilities.Public,
	})

	privateIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repo.ID,
		Title:      "Issue for reactions test",
		Visibility: entities.IssueVisibilities.Private,
	})

	t.Run("All reacted on public issue", func(t *testing.T) {
		addReactionReq := &pb.UpdateIssueReactionRequest{
			Id: grpc_marshalling.IDInverse(issue.ID),
			ReactionDelta: &pb.IdDelta{
				Id:     likeID,
				Action: pb.DeltaAction_ADD,
			},
		}

		for _, user := range []*entities.User{admin, krosh, barash} {
			userCtx := testutils.AuthorizeGRPC(user.Identity)
			_, err := client.UpdateReactions(userCtx, addReactionReq)
			require.NoError(t, err)
		}
		resp, err := client.ListReactedUsers(kroshCtx, &pb.ListReactedUsersRequest{
			Id:         grpc_marshalling.IDInverse(issue.ID),
			ReactionId: likeID,
		})

		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})

	t.Run("Krosh removed reaction", func(t *testing.T) {
		rmReactionReq := &pb.UpdateIssueReactionRequest{
			Id: grpc_marshalling.IDInverse(issue.ID),
			ReactionDelta: &pb.IdDelta{
				Id:     likeID,
				Action: pb.DeltaAction_REMOVE,
			},
		}

		_, err := client.UpdateReactions(kroshCtx, rmReactionReq)
		require.NoError(t, err)

		resp, err := client.ListReactedUsers(kroshCtx, &pb.ListReactedUsersRequest{
			Id:         grpc_marshalling.IDInverse(issue.ID),
			ReactionId: likeID,
		})

		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
	})

	t.Run("No access for private ticket", func(t *testing.T) {
		addReactionReq := &pb.UpdateIssueReactionRequest{
			Id: grpc_marshalling.IDInverse(privateIssue.ID),
			ReactionDelta: &pb.IdDelta{
				Id:     likeID,
				Action: pb.DeltaAction_ADD,
			},
		}
		_, err := client.UpdateReactions(adminCtx, addReactionReq)
		require.NoError(t, err)

		_, err = client.ListReactedUsers(kroshCtx, &pb.ListReactedUsersRequest{
			Id:         grpc_marshalling.IDInverse(privateIssue.ID),
			ReactionId: likeID,
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
}

func (suite *RwApiTestSuite) addReaction(ctx context.Context, issueID string) (*pb.Issue, error) {
	client := pb.NewIssueServiceClient(suite.grpcClient)
	req := &pb.UpdateIssueReactionRequest{
		Id: issueID,
		ReactionDelta: &pb.IdDelta{
			Id:     likeReactionID,
			Action: pb.DeltaAction_ADD,
		},
	}
	resp, err := client.UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(resp, &pb.Issue{})
}

func (suite *RwApiTestSuite) removeReaction(ctx context.Context, issueID string) (*pb.Issue, error) {
	client := pb.NewIssueServiceClient(suite.grpcClient)
	req := &pb.UpdateIssueReactionRequest{
		Id: issueID,
		ReactionDelta: &pb.IdDelta{
			Id:     likeReactionID,
			Action: pb.DeltaAction_REMOVE,
		},
	}
	resp, err := client.UpdateReactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return grpc_marshalling.OperationResponse(resp, &pb.Issue{})
}
