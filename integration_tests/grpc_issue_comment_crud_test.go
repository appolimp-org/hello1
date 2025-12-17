package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/entities/notifications"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type makeIssueCommentOptions struct {
	ID            uint64
	ParentID      *uint64
	IssueID       *uint64
	RepoID        *uint64
	Body          string
	AttachmentIDs []uint64
}

func (suite *RwApiTestSuite) TestGrpcCreateIssueComment() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "AdminIssue",
		Visibility: entities.IssueVisibilities.Public,
	})
	require.NoError(t, suite.RepoRepo.UpdateRepositoryByID(repoID).SetRepoVisibility(entities.Visibilities.Private).Commit(context.Background()))

	tt := map[string]struct {
		request         *pb.CreateIssueCommentRequest
		user            *entities.User
		verifyFromDb    func(*testing.T, *entities.IssueComment)
		verifyResponse  func(*testing.T, *pb.IssueComment)
		verifyException func(*testing.T, error)
		expectedStatus  codes.Code
	}{
		"happy path": {
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "admin comment",
			},
			user: suite.users.Admin,
			verifyFromDb: func(t *testing.T, comment *entities.IssueComment) {
				require.Equal(t, "admin comment", comment.Body)
				require.Equal(t, suite.users.Admin.ID, comment.AuthorID)
				require.Equal(t, suite.users.Admin.ID, comment.UpdatedBy)
			},
			verifyResponse: func(t *testing.T, comment *pb.IssueComment) {
				require.Equal(t, "admin comment", comment.Body)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.UpdatedBy)
			},
			expectedStatus: codes.OK,
		},
		"no access": {
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "no access comment",
			},
			user:           suite.users.PinPublic,
			expectedStatus: codes.PermissionDenied,
		},
		"no issue": {
			request: &pb.CreateIssueCommentRequest{
				IssueId: "12345",
				Body:    "no such issue",
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.Create(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				if tc.verifyException != nil {
					tc.verifyException(t, err)
				}
				return
			}

			commentResp, err := grpc_marshalling.OperationResponse(resp, &pb.IssueComment{})
			require.NoError(t, err)

			// yarequire.ProtoDumpFixture(t, commentResp)
			yarequire.ProtoCompareWithFixture(t, commentResp,
				protocmp.IgnoreFields(&pb.IssueComment{}, "id", "created_at", "updated_at"),
			)

			if tc.verifyResponse != nil {
				tc.verifyResponse(t, commentResp)
			}

			if tc.verifyFromDb != nil {
				commentID, err := grpc_marshalling.IDDirect(commentResp.Id)
				require.NoError(t, err)
				commentFromDb, err := suite.IssueCommentRepo.Get(ctx, commentID)
				require.NoError(t, err)
				tc.verifyFromDb(t, commentFromDb)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcCreateIssueCommentWithNotifications() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Krosh)
	issue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue",
		Visibility: entities.IssueVisibilities.Public,
		NotifyOpts: entities.NotifyOptions{
			SubscribeMe:       true,
			NotifySubscribers: true,
		},
	})
	require.NoError(t, suite.UserService.
		AddRelevantRepo(context.Background(), []uint64{suite.users.Kopatych.ID}, repoID))

	// krosh - subscriber
	// kopatych - relevant repo user
	// barash - passer

	type NotifyRequest struct {
		Mention bool
	}

	tt := map[string]struct {
		request          *pb.CreateIssueCommentRequest
		user             *entities.User
		expectedStatus   codes.Code
		expectedNotifies map[string]NotifyRequest
	}{
		"simple comment": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "Simple comment",
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: false,
				},
			},
		},
		"subscriber mentioned": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "Mention @" + suite.users.Krosh.Username,
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: true,
				},
			},
		},
		"not subscriber mentioned": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "Mention @" + suite.users.Kopatych.Username,
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: false,
				},
				suite.users.Kopatych.Username: {
					Mention: true,
				},
			},
		},
		"mention two users": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "@" + suite.users.Kopatych.Username + " and @" + suite.users.Krosh.Username + " mentioned",
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: true,
				},
				suite.users.Kopatych.Username: {
					Mention: true,
				},
			},
		},
		"do not mention passer": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "@" + suite.users.Kopatych.Username + " and @" + suite.users.Barash.Username + " mentioned",
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: false,
				},
				suite.users.Kopatych.Username: {
					Mention: true,
				},
			},
		},
		"fake mentioned": {
			user: suite.users.Admin,
			request: &pb.CreateIssueCommentRequest{
				IssueId: grpc_marshalling.IDInverse(issue.ID),
				Body:    "Mention @usernotfound",
			},
			expectedStatus: codes.OK,
			expectedNotifies: map[string]NotifyRequest{
				suite.users.Krosh.Username: {
					Mention: false,
				},
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, suite.ClearNotifyMessages())

			tc.request.NotificationOptions = &pb.NotificationOptions{
				SubscribeMe:       false,
				NotifySubscribers: true,
			}
			resp, err := client.Create(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}
			pbComment, err := grpc_marshalling.OperationResponse(resp, &pb.IssueComment{})
			require.NoError(t, err)

			suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
			notifyHistory := suite.NotifyMessages(ctx)
			require.Len(t, notifyHistory, len(tc.expectedNotifies))

			for _, notify := range notifyHistory {
				expectedNotify, ok := tc.expectedNotifies[notify.Receiver.ID]
				require.True(t, ok)
				require.Equal(t, "src.issue.comment", notify.Template)
				params := notify.Data.(*notifications.IssueCommentedParams)
				require.Equal(t, expectedNotify.Mention, params.Mention)
				require.Equal(t, pbComment.Id, params.Comments[0].ID)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetIssueComment() {
	t := suite.T()
	comment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		Body: "Some comment",
	})

	tt := map[string]struct {
		request         *pb.GetIssueCommentRequest
		user            *entities.User
		verifyFromDb    func(*testing.T, *entities.IssueComment)
		verifyResponse  func(*testing.T, *pb.IssueComment)
		verifyException func(*testing.T, error)
		expectedStatus  codes.Code
	}{
		"happy path": {
			request: &pb.GetIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(comment.ID),
			},
			user:           suite.users.Kopatych,
			expectedStatus: codes.OK,
			verifyResponse: func(t *testing.T, comment *pb.IssueComment) {
				require.Equal(t, "Some comment", comment.Body)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.UpdatedBy)
			},
			verifyFromDb: func(t *testing.T, comment *entities.IssueComment) {
				require.Equal(t, "Some comment", comment.Body)
				require.Equal(t, suite.users.Admin.ID, comment.AuthorID)
				require.Equal(t, suite.users.Admin.ID, comment.UpdatedBy)
			},
		},
		"not found": {
			request: &pb.GetIssueCommentRequest{
				CommentId: "12345",
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
	}

	client := pb.NewIssueCommentServiceClient(suite.grpcClient)

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.Get(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				if tc.verifyException != nil {
					tc.verifyException(t, err)
				}
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.IssueComment{}, "id", "created_at", "updated_at"),
			)

			if tc.verifyResponse != nil {
				tc.verifyResponse(t, resp)
			}

			if tc.verifyFromDb != nil {
				commentID, err := grpc_marshalling.IDDirect(resp.Id)
				require.NoError(t, err)
				commentFromDb, err := suite.IssueCommentRepo.Get(ctx, commentID)
				require.NoError(t, err)
				tc.verifyFromDb(t, commentFromDb)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcDeleteIssueComment() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)

	makeCommentID := func() uint64 {
		attachment := suite.UploadAttachment(suite.users.Admin.Identity, "cat.jpeg", entities.AttachmentScopes.IssueCommentAttachment)
		attachmentID, err := grpc_marshalling.IDDirect(attachment.ID)
		require.NoError(t, err)

		comment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
			Body:          "Some comment",
			AttachmentIDs: []uint64{attachmentID},
		})
		return comment.ID
	}

	tt := map[string]struct {
		commentIDGen   func() uint64
		request        *pb.DeleteIssueCommentRequest
		user           *entities.User
		expectedStatus codes.Code
	}{
		"happy path": {
			request: &pb.DeleteIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(makeCommentID()),
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
		},
		"no access for editing comments": {
			request: &pb.DeleteIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(makeCommentID()),
			},
			user:           suite.users.Kopatych,
			expectedStatus: codes.PermissionDenied,
		},
		"not the author": {
			request: &pb.DeleteIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(makeCommentID()),
			},
			user:           suite.users.Krosh,
			expectedStatus: codes.PermissionDenied,
		},
		"not found": {
			request: &pb.DeleteIssueCommentRequest{
				CommentId: "12345",
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			_, err := client.Delete(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}
			res, err := client.Get(ctx, &pb.GetIssueCommentRequest{
				CommentId: tc.request.CommentId,
			})
			require.NoError(t, err)
			require.Empty(t, res.Body)
			require.Empty(t, res.AttachmentsIds)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcIssueCommentParentId() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "SomeIssue",
		Visibility: entities.IssueVisibilities.Public,
	})

	otherIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "OtherIssue",
		Visibility: entities.IssueVisibilities.Public,
	})

	parentCommentID := grpc_marshalling.IDInverse(
		suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment",
		}).ID)

	notExistingParentID := "12345"

	tt := map[string]struct {
		request        *pb.CreateIssueCommentRequest
		expectedStatus codes.Code
	}{
		"happy path": {
			request: &pb.CreateIssueCommentRequest{
				IssueId:  grpc_marshalling.IDInverse(issue.ID),
				ParentId: &parentCommentID,
				Body:     "Happy comment",
			},
			expectedStatus: codes.OK,
		},
		"parent not found": {
			request: &pb.CreateIssueCommentRequest{
				IssueId:  grpc_marshalling.IDInverse(issue.ID),
				ParentId: &notExistingParentID,
				Body:     "Not found comment",
			},
			expectedStatus: codes.NotFound,
		},
		"issue mismatch": {
			request: &pb.CreateIssueCommentRequest{
				IssueId:  grpc_marshalling.IDInverse(otherIssue.ID),
				ParentId: &parentCommentID,
				Body:     "Not happy comment",
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
			_, err := client.Create(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcIssueCommentUpdate() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "SomeIssue",
		Visibility: entities.IssueVisibilities.Public,
	})

	adminComment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "Some comment",
	})
	kopComment := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "Some comment",
	})

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	deletedComment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "Some comment",
	})
	require.NoError(t, suite.IssueCommentRepo.Delete(ctx, deletedComment.ID))

	updatedBody := "Updated comment"

	tt := map[string]struct {
		request        *pb.UpdateIssueCommentRequest
		verifyFromDb   func(*testing.T, *entities.IssueComment)
		verifyResponse func(*testing.T, *pb.IssueComment)
		user           *entities.User
		expectedStatus codes.Code
	}{
		"happy path": {
			request: &pb.UpdateIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(adminComment.ID),
				Body:      updatedBody,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"body"},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			verifyResponse: func(t *testing.T, comment *pb.IssueComment) {
				require.Equal(t, updatedBody, comment.Body)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Admin.ID), comment.UpdatedBy)
			},
			verifyFromDb: func(t *testing.T, comment *entities.IssueComment) {
				require.Equal(t, updatedBody, comment.Body)
				require.Equal(t, suite.users.Admin.ID, comment.AuthorID)
				require.Equal(t, suite.users.Admin.ID, comment.UpdatedBy)
			},
		},
		"not admin can edit his comment": {
			request: &pb.UpdateIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(kopComment.ID),
				Body:      updatedBody,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"body"},
				},
			},
			user:           suite.users.Kopatych,
			expectedStatus: codes.OK,
			verifyResponse: func(t *testing.T, comment *pb.IssueComment) {
				require.Equal(t, updatedBody, comment.Body)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Kopatych.ID), comment.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Kopatych.ID), comment.UpdatedBy)
			},
			verifyFromDb: func(t *testing.T, comment *entities.IssueComment) {
				require.Equal(t, updatedBody, comment.Body)
				require.Equal(t, suite.users.Kopatych.ID, comment.AuthorID)
				require.Equal(t, suite.users.Kopatych.ID, comment.UpdatedBy)
			},
		},
		"no access": {
			request: &pb.UpdateIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(adminComment.ID),
				Body:      updatedBody,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"body"},
				},
			},
			user:           suite.users.Kopatych,
			expectedStatus: codes.PermissionDenied,
		},
		"not exists": {
			request: &pb.UpdateIssueCommentRequest{
				CommentId: "12345",
				Body:      updatedBody,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"body"},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
		"deleted": {
			request: &pb.UpdateIssueCommentRequest{
				CommentId: grpc_marshalling.IDInverse(deletedComment.ID),
				Body:      updatedBody,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"body"},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.Update(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			commentResp, err := grpc_marshalling.OperationResponse(resp, &pb.IssueComment{})
			require.NoError(t, err)

			// yarequire.ProtoDumpFixture(t, commentResp)
			yarequire.ProtoCompareWithFixture(t, commentResp,
				protocmp.IgnoreFields(&pb.IssueComment{}, "id", "created_at", "updated_at"),
			)

			if tc.verifyResponse != nil {
				tc.verifyResponse(t, commentResp)
			}

			commentID, _ := grpc_marshalling.IDDirect(commentResp.Id)
			commentFromDb, err := suite.IssueCommentRepo.Get(ctx, commentID)
			require.NoError(t, err)

			compareComments(t, commentFromDb, commentResp)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcIssueCommentsList() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	t.Run("Empty list", func(t *testing.T) {
		issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeIssue",
			Visibility: entities.IssueVisibilities.Public,
		})
		comments, err := client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListIssueCommentsRequest{
			IssueId: grpc_marshalling.IDInverse(issue.ID),
		})
		require.NoError(t, err)
		require.Empty(t, comments.Comments)
	})

	t.Run("Happy path", func(t *testing.T) {
		user := suite.users.Kopatych
		issue := suite.makeIssue(user, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeIssue",
			Visibility: entities.IssueVisibilities.Public,
		})

		comment1 := suite.makeIssueComment(user, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment",
		})
		comment2 := suite.makeIssueComment(user, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment 2",
		})
		comment1Child := suite.makeIssueComment(user, &makeIssueCommentOptions{
			IssueID:  &issue.ID,
			Body:     "Some child",
			ParentID: &comment1.ID,
		})

		comments, err := client.List(testutils.AuthorizeGRPC(user.Identity), &pb.ListIssueCommentsRequest{
			IssueId: grpc_marshalling.IDInverse(issue.ID),
		})

		require.NoError(t, err)
		require.Len(t, comments.Comments, 3)

		compareComments(t, comment1, comments.Comments[0])
		compareComments(t, comment2, comments.Comments[1])
		compareComments(t, comment1Child, comments.Comments[2])
	})

	t.Run("Issue not exists", func(t *testing.T) {
		user := suite.users.Kopatych
		issue := suite.makeIssue(user, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeIssue",
			Visibility: entities.IssueVisibilities.Public,
		})

		suite.makeIssueComment(user, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment",
		})
		suite.makeIssueComment(user, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment 2",
		})

		_, err := client.List(testutils.AuthorizeGRPC(user.Identity), &pb.ListIssueCommentsRequest{
			IssueId: "12345",
		})

		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("Pagination and sort", func(t *testing.T) {
		user := suite.users.Kopatych
		issue := suite.makeIssue(user, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeIssue",
			Visibility: entities.IssueVisibilities.Public,
		})

		comments := make([]*entities.IssueComment, 0, 6)

		for i := 0; i < 6; i++ {
			comments = append(comments, suite.makeIssueComment(user, &makeIssueCommentOptions{
				IssueID: &issue.ID,
				Body:    "Some comment",
			}))
		}

		reqPageSize := uint64(3)
		commentsResp, err := client.List(testutils.AuthorizeGRPC(user.Identity), &pb.ListIssueCommentsRequest{
			IssueId:  grpc_marshalling.IDInverse(issue.ID),
			PageSize: &reqPageSize,
			SortBy: []*pagination_pb.SortOption{
				{
					Column:    "created_at",
					Direction: pagination_pb.SortOption_DESC,
				},
			},
		})

		require.NoError(t, err)
		require.Len(t, commentsResp.Comments, 3)
		require.NotEmpty(t, commentsResp.NextPageToken)
		compareComments(t, comments[5], commentsResp.Comments[0])
		compareComments(t, comments[4], commentsResp.Comments[1])
		compareComments(t, comments[3], commentsResp.Comments[2])

		commentsResp, err = client.List(testutils.AuthorizeGRPC(user.Identity), &pb.ListIssueCommentsRequest{
			IssueId:   grpc_marshalling.IDInverse(issue.ID),
			PageSize:  &reqPageSize,
			PageToken: &commentsResp.NextPageToken,
			SortBy: []*pagination_pb.SortOption{
				{
					Column:    "author_id",
					Direction: pagination_pb.SortOption_DESC,
				},
			},
		})

		require.NoError(t, err)
		require.Len(t, commentsResp.Comments, 3)
		compareComments(t, comments[2], commentsResp.Comments[0])
		compareComments(t, comments[1], commentsResp.Comments[1])
		compareComments(t, comments[0], commentsResp.Comments[2])
	})

}

func (suite *RwApiTestSuite) TestIssueCommentsGetBulk() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "SomeIssue",
		Visibility: entities.IssueVisibilities.Public,
	})

	comments := make([]*entities.IssueComment, 0, 6)

	for i := 0; i < 6; i++ {
		comments = append(comments, suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
			IssueID: &issue.ID,
			Body:    "Some comment " + strconv.Itoa(i),
		}))
	}

	t.Run("Happy path", func(t *testing.T) {
		ids := functools.Map(comments, func(comment *entities.IssueComment) string {
			return grpc_marshalling.IDInverse(comment.ID)
		})
		resp, err := client.GetBulk(testutils.AuthorizeGRPC(suite.users.Admin.Identity), &pb.GetBulkIssueCommentsRequest{
			IssueId:    grpc_marshalling.IDInverse(issue.ID),
			CommentIds: ids,
		})

		require.NoError(t, err)
		require.Len(t, resp.Comments, 6)
		for i, comment := range comments {
			compareComments(t, comment, resp.Comments[i])
		}
	})

	t.Run("Some fake comments", func(t *testing.T) {
		otherIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeOtherIssue",
			Visibility: entities.IssueVisibilities.Public,
		})

		otherIssueComment := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
			IssueID: &otherIssue.ID,
			Body:    "Some comment",
		})

		idsWithFake := functools.Map([]uint64{otherIssueComment.ID, 1234, comments[0].ID, comments[1].ID}, func(commentId uint64) string {
			return grpc_marshalling.IDInverse(commentId)
		})

		resp, err := client.GetBulk(testutils.AuthorizeGRPC(suite.users.Admin.Identity), &pb.GetBulkIssueCommentsRequest{
			IssueId:    grpc_marshalling.IDInverse(issue.ID),
			CommentIds: idsWithFake,
		})
		require.NoError(t, err)
		require.Len(t, resp.Comments, 2)

		commentsResp := resp.Comments

		compareComments(t, comments[0], commentsResp[0])
		compareComments(t, comments[1], commentsResp[1])
	})

}

func compareComments(t *testing.T, dbComment *entities.IssueComment, respComment *pb.IssueComment) {
	commentID, err := grpc_marshalling.IDDirect(respComment.Id)
	require.NoError(t, err)
	require.Equal(t, dbComment.ID, commentID)
	require.Equal(t, dbComment.Body, respComment.Body)

	parentID, err := grpc_marshalling.IDNullableDirect(respComment.ParentId)
	require.NoError(t, err)
	require.Equal(t, dbComment.ParentID, parentID)

	updatedByID, err := grpc_marshalling.IDDirect(respComment.UpdatedBy)
	require.NoError(t, err)
	require.Equal(t, dbComment.UpdatedBy, updatedByID)

	authorID, err := grpc_marshalling.IDDirect(respComment.AuthorId)
	require.NoError(t, err)
	require.Equal(t, dbComment.AuthorID, authorID)
}

func (suite *RwApiTestSuite) makeIssueComment(author *entities.User, opts *makeIssueCommentOptions) *entities.IssueComment {
	t := suite.T()
	repoID := suite.repos.Alpha.ID
	if opts.RepoID != nil {
		repoID = *opts.RepoID
	}
	issueID := opts.IssueID

	if issueID == nil {
		issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "SomeIssue",
			Visibility: entities.IssueVisibilities.Public,
		})
		issueID = &issue.ID
	}

	commentID, err := suite.IssueCommentService.Create(
		context.Background(),
		&entities.IssueComment{
			ID:        opts.ID,
			IssueID:   *issueID,
			ParentID:  opts.ParentID,
			Body:      opts.Body,
			AuthorID:  author.ID,
			UpdatedBy: author.ID,
		},
		opts.AttachmentIDs,
		&entities.Issue{
			ID:     *issueID,
			RepoID: repoID,
		}, author,
		entities.NotifyOptions{},
	)
	require.NoError(t, err)

	comment, err := suite.IssueCommentRepo.Get(context.Background(), commentID)
	require.NoError(t, err)

	return comment
}
