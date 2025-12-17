package integrationtests

import (
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"common/functools"
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"
)

func (suite *RwApiTestSuite) TestComments_GetBulk() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	commentIDs := make([]uint64, 0, 6)
	publishedCommentIDs := make([]uint64, 0, 4)
	for _, user := range []*entities.User{suite.users.Kopatych, suite.users.Krosh} {
		feed := suite.makeSmallPrFeed(user, pr)
		commentIDs = append(commentIDs, feed...)
		publishedCommentIDs = append(publishedCommentIDs, feed[0], feed[2])
	}

	pr2 := suite.makePullRequest(suite.users.Kopatych, nil)
	extraCommentIDs := suite.makeSmallPrFeed(suite.users.Kopatych, pr2)

	for _, test := range []struct {
		name         string
		userIdentity entities.UserIdentity
		requestIDs   []uint64
		expectedIDs  []uint64
	}{
		{
			name:         "admin get all",
			userIdentity: suite.users.Kopatych.Identity,
			requestIDs:   commentIDs,
			expectedIDs:  append(publishedCommentIDs, commentIDs[1]),
		},
		{
			name:         "contributor get all",
			userIdentity: suite.users.Krosh.Identity,
			requestIDs:   commentIDs,
			expectedIDs:  append(publishedCommentIDs, commentIDs[4]),
		},
		{
			name:         "other pr not included",
			userIdentity: suite.users.Slowpoke.Identity,
			requestIDs:   append(commentIDs[:], extraCommentIDs...),
			expectedIDs:  publishedCommentIDs,
		},
		{
			name:         "non-existent comment",
			userIdentity: suite.users.Kopatych.Identity,
			requestIDs:   append(commentIDs[:], 1, 2, 3),
			expectedIDs:  append(publishedCommentIDs, commentIDs[1]),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			comments, err := client.GetBulk(testutils.AuthorizeGRPC(test.userIdentity), &pb.GetBulkCommentRequest{
				PrId:       grpc2.MarshalID(pr.ID),
				CommentIds: functools.Map(test.requestIDs, grpc2.MarshalID),
			})
			require.NoError(t, err)

			expectedSet := functools.SliceToSetF(test.expectedIDs, grpc2.MarshalID)
			commentsSet := functools.SliceToSetF(comments.Comments, func(c *pb.PRComment) string {
				return c.Id
			})
			require.Equal(t, expectedSet, commentsSet)
		})
	}

	t.Run("request too large", func(t *testing.T) {
		requestIDs := make([]string, 0, 1001)
		for i := 1; i <= 1001; i++ {
			requestIDs = append(requestIDs, grpc2.MarshalID(uint64(i)))
		}

		_, err := client.GetBulk(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.GetBulkCommentRequest{
			PrId:       grpc2.MarshalID(pr.ID),
			CommentIds: requestIDs,
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})
}

func (suite *RwApiTestSuite) TestComments_List() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	pr := suite.makePullRequest(suite.users.Kopatych, nil)
	pbPrID := grpc2.MarshalID(pr.ID)

	kopatychFeed := suite.makeSmallPrFeed(suite.users.Kopatych, pr)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
	kroshFeed := suite.makeSmallPrFeed(suite.users.Krosh, pr)
	publishedCommentIDs := []uint64{kopatychFeed[0], kopatychFeed[2], kroshFeed[0], kroshFeed[2]}

	for _, test := range []struct {
		name         string
		userIdentity entities.UserIdentity
		onlyDrafts   bool
		expectedIDs  []uint64
	}{
		{
			name:         "viewer lists only published",
			userIdentity: suite.users.Slowpoke.Identity,
			expectedIDs:  publishedCommentIDs,
		},
		{
			name:         "user gets published and own drafts",
			userIdentity: suite.users.Kopatych.Identity,
			expectedIDs:  append(publishedCommentIDs, kopatychFeed[1]),
		},
		{
			name:         "user lists only his drafts",
			userIdentity: suite.users.Krosh.Identity,
			onlyDrafts:   true,
			expectedIDs:  []uint64{kroshFeed[1]},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			comments, err := client.List(testutils.AuthorizeGRPC(test.userIdentity), &pb.ListCommentsRequest{
				PrId:       pbPrID,
				OnlyDrafts: test.onlyDrafts,
			})
			require.NoError(t, err)

			expectedSet := functools.SliceToSetF(test.expectedIDs, grpc2.MarshalID)
			commentsSet := functools.SliceToSetF(comments.Comments, func(c *pb.PRComment) string {
				return c.Id
			})
			require.Equal(t, expectedSet, commentsSet)
		})
	}

	t.Run("empty list", func(t *testing.T) {
		pr = suite.makePullRequest(suite.users.Kopatych, nil)
		comments, err := client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListCommentsRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Empty(t, comments.Comments)
	})

	t.Run("sort and pagination", func(t *testing.T) {
		dir := pagination_pb.SortOption_DESC
		if suite.users.Krosh.ID > suite.users.Kopatych.ID {
			dir = pagination_pb.SortOption_ASC
		}

		comments, err := client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListCommentsRequest{
			PrId:     pbPrID,
			PageSize: utils.PtrFromValue(uint64(3)),
			SortBy: []*pagination_pb.SortOption{
				{
					Column:    "author_id",
					Direction: dir,
				},
			},
		})
		require.NoError(t, err)
		require.NotEmpty(t, comments.NextPageToken)

		expectedIDs := functools.SliceToSetF(kopatychFeed, grpc2.MarshalID)
		commentIDs := functools.SliceToSetF(comments.Comments, func(c *pb.PRComment) string {
			return c.Id
		})
		require.Equal(t, expectedIDs, commentIDs)

		comments, err = client.List(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.ListCommentsRequest{
			PrId:     pbPrID,
			PageSize: utils.PtrFromValue(uint64(3)),
			SortBy: []*pagination_pb.SortOption{
				{
					Column:    "author_id",
					Direction: dir,
				},
			},
			PageToken: &comments.NextPageToken,
		})
		require.NoError(t, err)
		require.Empty(t, comments.NextPageToken)

		expectedIDs = functools.SliceToSetF(append(kroshFeed[:1], kroshFeed[2]), grpc2.MarshalID)
		commentIDs = functools.SliceToSetF(comments.Comments, func(c *pb.PRComment) string {
			return c.Id
		})
		require.Equal(t, expectedIDs, commentIDs)
	})
}

func (suite *RwApiTestSuite) TestComments_CreateAndGet() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych

	pr := suite.makePullRequest(user, nil)
	anotherPr := suite.makePullRequest(user, nil)

	pbPrID := grpc2.MarshalID(pr.ID)
	anotherPrID := grpc2.MarshalID(anotherPr.ID)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	parent := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Parent comment", Draft: true})
	pbParentID := grpc2.MarshalID(parent.ID)

	notExistingParentID := "12345"
	for _, test := range []struct {
		name            string
		request         *pb.CreateCommentRequest
		expectedCode    codes.Code
		expectedComment *pb.PRComment
	}{
		{
			name: "unpublished with parent ok",
			request: &pb.CreateCommentRequest{
				PrId:     pbPrID,
				Body:     "unpublished with parent",
				ParentId: &pbParentID,
				Publish:  false,
			},
			expectedCode: codes.OK,
			expectedComment: &pb.PRComment{
				Body:        "unpublished with parent",
				ParentId:    &pbParentID,
				AuthorId:    grpc2.MarshalID(user.ID),
				IsPublished: utils.PtrFromValue(false),
				Type:        pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		},
		{
			name: "published without parent ok",
			request: &pb.CreateCommentRequest{
				PrId:    pbPrID,
				Body:    "published without parent",
				Publish: true,
			},
			expectedCode: codes.OK,
			expectedComment: &pb.PRComment{
				Body:        "published without parent",
				AuthorId:    grpc2.MarshalID(user.ID),
				IsPublished: utils.PtrFromValue(true),
				Type:        pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		},
		{
			name: "published needs resolution ok",
			request: &pb.CreateCommentRequest{
				PrId:           pbPrID,
				Body:           "published needs resolution",
				NeedResolution: true,
				Publish:        true,
			},
			expectedCode: codes.OK,
			expectedComment: &pb.PRComment{
				Body:           "published needs resolution",
				NeedResolution: true,
				AuthorId:       grpc2.MarshalID(user.ID),
				IsPublished:    utils.PtrFromValue(true),
				IsResolved:     utils.PtrFromValue(false),
				Type:           pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		},
		{
			name: "unpublished needs resolution ok",
			request: &pb.CreateCommentRequest{
				PrId:           pbPrID,
				Body:           "unpublished needs resolution",
				NeedResolution: true,
				Publish:        false,
			},
			expectedCode: codes.OK,
		},
		{
			name: "no body invalid",
			request: &pb.CreateCommentRequest{
				PrId: pbPrID,
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "unknown pr not found",
			request: &pb.CreateCommentRequest{
				PrId: "0",
				Body: "X",
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "invalid slugs not found",
			request: &pb.CreateCommentRequest{
				PrId: pbPrID,
				Parents: &pb.RepositoryFullSlug{
					OrgSlug:  suite.repos.Alpha.OrgSlug,
					RepoSlug: suite.repos.Alpha.Slug + "x",
				},
				Body: "X",
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "parent comment pr mismatch",
			request: &pb.CreateCommentRequest{
				PrId:     anotherPrID,
				ParentId: &pbParentID,
				Body:     "X",
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "parent comment not found",
			request: &pb.CreateCommentRequest{
				PrId:     pbPrID,
				ParentId: &notExistingParentID,
				Body:     "X",
			},
			expectedCode: codes.NotFound,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			op, err := client.Create(ctx, test.request)
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)

			if test.expectedComment == nil {
				return
			}

			comment, err := op.GetResponse().UnmarshalNew()
			require.NoError(t, err)
			cmt, ok := comment.(*pb.PRComment)
			require.True(t, ok)

			yarequire.ProtoCmp(
				t, test.expectedComment, cmt,
				protocmp.IgnoreFields(
					&pb.PRComment{},
					"created_at", "updated_at",
					"is_deleted", "is_outdated",
					"iteration", "id",
				),
			)

			cmtGet, err := client.Get(ctx, &pb.GetCommentRequest{
				PrId:      pbPrID,
				CommentId: cmt.Id,
			})
			require.NoError(t, err)
			yarequire.ProtoEqual(t, cmt, cmtGet)
		})
	}

	t.Run("get with invalid pr", func(t *testing.T) {
		_, err := client.Get(ctx, &pb.GetCommentRequest{
			PrId:      "0",
			CommentId: pbParentID,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})
	t.Run("other user get unpublished", func(t *testing.T) {
		_, err := client.Get(testutils.AuthorizeGRPC(suite.users.Krosh.Identity), &pb.GetCommentRequest{
			PrId:      pbPrID,
			CommentId: pbParentID,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
	t.Run("other user get published", func(t *testing.T) {
		_, err := client.PublishDrafts(ctx, &pb.PublishDraftsRequest{PrId: pbPrID})
		require.NoError(t, err)

		cmt, err := client.Get(testutils.AuthorizeGRPC(suite.users.Krosh.Identity), &pb.GetCommentRequest{
			PrId:      pbPrID,
			CommentId: pbParentID,
		})
		require.NoError(t, err)
		require.Equal(t, pbParentID, cmt.Id)
	})

	t.Run("contributor can create", func(t *testing.T) {
		_, err := client.Create(testutils.AuthorizeGRPC(suite.users.Krosh.Identity), &pb.CreateCommentRequest{
			PrId: pbPrID,
			Body: "X",
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
	})

	t.Run("non contributor can't create", func(t *testing.T) {
		_, err := client.Create(context.Background(), &pb.CreateCommentRequest{
			PrId: pbPrID,
			Body: "X",
		})
		yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
	})
}

func (suite *RwApiTestSuite) TestComments_GetAllFields() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	suite.addRole(t, user, suite.repos.PrValidation, iam.Roles.RepositoriesDeveloper)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	pr := suite.makePullRequest(user, &makePrOptions{
		Repo:   suite.repos.PrValidation,
		Source: "validate-2",
		Target: "validate-1",
	})
	suite.mustBash(suite.repos.PrValidation, `
		git checkout validate-2
		echo something >> file1.txt
		git add .
		git commit -m "Something"
	`)

	pbPrID := grpc2.MarshalID(pr.ID)
	iteration := grpc2.MarshalID(pr.Iteration)

	parent := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Parent", NeedResolution: true})
	op, err := client.Create(ctx, &pb.CreateCommentRequest{
		PrId:      pbPrID,
		Body:      "Some comment body",
		ParentId:  utils.PtrFromValue(grpc2.MarshalID(parent.ID)),
		Publish:   true,
		Iteration: &iteration,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "file1.txt",
			Position: &pb.DiffPos{
				From: 1,
				To:   2,
				Side: pb.DiffPos_SOURCE,
			},
		},
		NeedResolution: true,
	})
	require.NoError(t, err)

	suite.mustBash(suite.repos.PrValidation, `
		git checkout validate-2
		echo something > file1.txt
		git add .
		git commit -m "Something (2)"
	`)

	tmp, err := op.GetResponse().UnmarshalNew()
	require.NoError(t, err)
	comment, _ := client.Get(ctx, &pb.GetCommentRequest{
		PrId:      pbPrID,
		CommentId: tmp.(*pb.PRComment).Id,
	})
	require.NoError(t, err)

	yarequire.ProtoCmp(
		t, &pb.PRComment{
			Id:       comment.Id,
			Body:     "Some comment body",
			ParentId: utils.PtrFromValue(grpc2.MarshalID(parent.ID)),
			Anchor: &pb.Anchor{
				Path: "file1.txt",
				Position: &pb.DiffPos{
					From: 1,
					To:   2,
					Side: pb.DiffPos_SOURCE,
				},
				Hunk: &pb.Hunk{
					FromStart: 1,
					FromCount: 1,
					ToStart:   1,
					ToCount:   1,
					Patch:     "-v1\n+v2\n",
				},
			},
			Iteration:      pr.Iteration,
			NeedResolution: true,
			AuthorId:       grpc2.MarshalID(user.ID),
			IsResolved:     utils.PtrFromValue(false),
			IsPublished:    utils.PtrFromValue(true),
			IsDeleted:      utils.PtrFromValue(false),
			Type:           pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
		}, comment,
		protocmp.IgnoreFields(&pb.PRComment{}, "created_at", "updated_at", "is_outdated"),
	)
}

func (suite *RwApiTestSuite) TestComments_Delete() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Krosh
	repo := suite.repos.Alpha

	suite.addRole(t, user, repo, iam.Roles.RepositoriesContributor)
	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Slowpoke, repo, iam.Roles.RepositoriesContributor)

	pr := suite.makePullRequest(user, nil)
	pbPrID := grpc2.MarshalID(pr.ID)

	parent := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Parent"})
	comments := []*entities.PullRequestComment{
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "A", Draft: true}),
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "B"}),
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "C", NeedResolution: true}),
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "D", NeedResolution: true, ParentID: &parent.ID}),
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "E", Draft: true, ParentID: &parent.ID}),
		suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "F", Type: entities.PullRequestCommentTypes.AppSec}),
	}

	for _, test := range []struct {
		name         string
		userIdentity entities.UserIdentity
		comment      *entities.PullRequestComment
		expectedCode codes.Code
	}{
		{
			name:         "contributor can't delete others",
			userIdentity: suite.users.Slowpoke.Identity,
			comment:      comments[1],
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "admin can delete others published",
			userIdentity: suite.users.Kopatych.Identity,
			comment:      comments[1],
			expectedCode: codes.OK,
		},
		{
			name:         "admin can't delete others drafts",
			userIdentity: suite.users.Kopatych.Identity,
			comment:      comments[0],
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "self can delete published",
			userIdentity: suite.users.Krosh.Identity,
			comment:      comments[2],
			expectedCode: codes.OK,
		},
		{
			name:         "self can delete draft",
			userIdentity: suite.users.Krosh.Identity,
			comment:      comments[0],
			expectedCode: codes.OK,
		},
		{
			name:         "non default comment can't be deleted",
			userIdentity: suite.users.Krosh.Identity,
			comment:      comments[5],
			expectedCode: codes.PermissionDenied,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(test.userIdentity)

			_, err := client.Delete(ctx, &pb.DeleteCommentRequest{
				PrId:                pbPrID,
				CommentId:           grpc2.MarshalID(test.comment.ID),
				NotificationOptions: testutils.SkipNotificationPb,
			})
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)
			if err != nil {
				return
			}

			comment, err := client.Get(ctx, &pb.GetCommentRequest{
				PrId:      pbPrID,
				CommentId: grpc2.MarshalID(test.comment.ID),
			})
			if test.comment.IsPublished {
				require.NoError(t, err)
				require.Equal(t, "", comment.Body)
			} else {
				yarequire.ProtoStatusEqual(t, codes.NotFound, err)
			}
		})
	}

	t.Run("self can delete parent comment", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(user.Identity)

		_, err := client.Delete(ctx, &pb.DeleteCommentRequest{
			PrId:                pbPrID,
			CommentId:           grpc2.MarshalID(parent.ID),
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		child, _ := client.Get(ctx, &pb.GetCommentRequest{
			PrId:      pbPrID,
			CommentId: grpc2.MarshalID(comments[4].ID),
		})
		require.NoError(t, err)
		require.NotNil(t, child.ParentId)
		require.Equal(t, grpc2.MarshalID(parent.ID), *child.ParentId)
	})

	t.Run("delete already deleted", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(user.Identity)

		for _, draft := range []bool{false, true} {
			comment := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{
				Body:  "Marked for deletion",
				Draft: draft,
			})
			_, err := client.Delete(ctx, &pb.DeleteCommentRequest{
				PrId:      pbPrID,
				CommentId: grpc2.MarshalID(comment.ID),
			})
			require.NoError(t, err, "draft: %b", draft)

			_, err = client.Delete(ctx, &pb.DeleteCommentRequest{
				PrId:      pbPrID,
				CommentId: grpc2.MarshalID(comment.ID),
			})
			yarequire.ProtoStatusEqual(t, codes.NotFound, err)
		}
	})
}

func (suite *RwApiTestSuite) TestComments_Update() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	admin := suite.users.Admin
	ctx := testutils.AuthorizeGRPC(user.Identity)

	pr := suite.makePullRequest(user, nil)
	pbPrID := grpc2.MarshalID(pr.ID)

	comment := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Comment"})
	pbCommentID := grpc2.MarshalID(comment.ID)
	otherComment := suite.makePrCommentGRPC(t, suite.users.Krosh, pr, &makePrCommentOptions{Body: "Krosh's comment", NeedResolution: true})
	pbOtherCommentID := grpc2.MarshalID(otherComment.ID)

	nonDefaultComment := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Non default comment", Type: entities.PullRequestCommentTypes.AppSec, NeedResolution: true})
	pbNonDefaultCommentID := grpc2.MarshalID(nonDefaultComment.ID)
	resolveUpdate := true

	bigBody := strings.Repeat("a", 65535)

	for _, test := range []struct {
		name         string
		request      *pb.UpdateCommentRequest
		asUser       *entities.User
		expectedCode codes.Code
		check        func(*testing.T, *entities.PullRequestComment)
	}{
		{
			name: "no updates",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbCommentID,
				UpdateMask: nil,
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.Equal(t, comment.Body, c.Body)
				require.Equal(t, comment.NeedResolution, c.NeedResolution)
				require.Equal(t, comment.IsResolved, c.IsResolved)
			},
		},
		{
			name: "update only body with field mask",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbCommentID,
				Body:       utils.PtrFromValue("Edited"),
				IsResolved: utils.PtrFromValue(true),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"body"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.Equal(t, "Edited", c.Body)
				require.Equal(t, comment.NeedResolution, c.NeedResolution)
				require.Equal(t, comment.IsResolved, c.IsResolved)
			},
		},
		{
			name: "update bigBody",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbCommentID,
				Body:       &bigBody,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"body"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.Equal(t, bigBody, c.Body)
			},
		},
		{
			name: "update bigBody+1",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbCommentID,
				Body:       utils.PtrFromValue(bigBody + "limit"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"body"}},
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "update needs resolution",
			request: &pb.UpdateCommentRequest{
				PrId:           pbPrID,
				CommentId:      pbCommentID,
				NeedResolution: utils.PtrFromValue(true),
				UpdateMask:     &fieldmaskpb.FieldMask{Paths: []string{"need_resolution"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.True(t, c.NeedResolution)
				require.False(t, c.IsResolved)
			},
		},
		{
			name: "update resolve",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbCommentID,
				IsResolved: utils.PtrFromValue(true),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.True(t, c.NeedResolution)
				require.True(t, c.IsResolved)
			},
		},
		{
			name: "remove need for resolution and resolution",
			request: &pb.UpdateCommentRequest{
				PrId:           pbPrID,
				CommentId:      pbCommentID,
				NeedResolution: utils.PtrFromValue(false),
				IsResolved:     utils.PtrFromValue(false),
				UpdateMask:     &fieldmaskpb.FieldMask{Paths: []string{"is_resolved", "need_resolution"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.False(t, c.NeedResolution)
				require.False(t, c.IsResolved)
			},
		},
		{
			name: "comment id not found",
			request: &pb.UpdateCommentRequest{
				PrId:      pbPrID,
				CommentId: "1",
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "invalid parent slugs",
			request: &pb.UpdateCommentRequest{
				Parents: &pb.RepositoryFullSlug{
					OrgSlug:  suite.repos.Alpha.OrgSlug + "x",
					RepoSlug: suite.repos.Alpha.Slug,
				},
				PrId:      pbPrID,
				CommentId: pbCommentID,
			},
			expectedCode: codes.NotFound,
		},
		// TODO probably should pass but I guess for now we allow it
		//{
		//	name: "can not resolve if no resolution is needed",
		//	request: &pb.UpdateCommentRequest{
		//		PrId:       pbPrID,
		//		CommentId:  pbCommentID,
		//		IsResolved: utils.PtrFromValue(true),
		//		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
		//	},
		//	expectedCode: codes.InvalidArgument,
		//},
		{
			name: "can not resolve non-authored comments",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbOtherCommentID,
				IsResolved: utils.PtrFromValue(true),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "can not edit non-authored comments",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbOtherCommentID,
				Body:       utils.PtrFromValue("New body"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"body"}},
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name:   "admin can resolve non-authored comments",
			asUser: admin,
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbOtherCommentID,
				IsResolved: utils.PtrFromValue(true),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.True(t, c.NeedResolution)
				require.True(t, c.IsResolved)
			},
		},
		{
			name: "non default comment types can't be edited",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbNonDefaultCommentID,
				Body:       utils.PtrFromValue("New body"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"body"}},
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "non default comment can't be resolved with other fields",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbNonDefaultCommentID,
				IsResolved: &resolveUpdate,
				Body:       utils.PtrFromValue("New body"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved", "body"}},
			},
			expectedCode: codes.PermissionDenied,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.False(t, c.IsResolved)
				require.Equal(t, nonDefaultComment.Body, c.Body)
			},
		},
		{
			name: "non default comment can be resolved",
			request: &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  pbNonDefaultCommentID,
				IsResolved: &resolveUpdate,
				Body:       utils.PtrFromValue("New body"),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_resolved"}},
			},
			expectedCode: codes.OK,
			check: func(t *testing.T, c *entities.PullRequestComment) {
				require.True(t, c.IsResolved)
				require.Equal(t, nonDefaultComment.Body, c.Body)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx2 := ctx
			if test.asUser != nil {
				ctx2 = testutils.AuthorizeGRPC(test.asUser.Identity)
			}

			_, err := client.Update(ctx2, test.request)
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)

			commentID, err := grpc_marshalling.IDDirect(test.request.CommentId)
			require.NoError(t, err)

			if test.check != nil {
				c, err := suite.PullRequestCommentService.Get(ctx2, pr.RepoID, pr.ID, commentID)
				require.NoError(t, err)

				test.check(t, c)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestComments_UpdatePermissions() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych

	pr := suite.makePullRequest(user, nil)
	pbPrID := grpc2.MarshalID(pr.ID)
	suite.mustBash(suite.repos.PrValidation, `
		git checkout validate-2
		echo something >> file1.txt
		git add .
		git commit -m "Something"
	`)

	comment := suite.makePrCommentGRPC(t, user, pr, nil)

	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Slowpoke, suite.repos.Alpha, iam.Roles.RepositoriesViewer)

	for _, test := range []struct {
		name         string
		userIdentity entities.UserIdentity
		resolve      bool
		editBody     bool
		expectedCode codes.Code
	}{
		{
			name:         "viewer can't edit",
			userIdentity: suite.users.Slowpoke.Identity,
			resolve:      false,
			editBody:     true,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "viewer can't resolve",
			userIdentity: suite.users.Slowpoke.Identity,
			resolve:      true,
			editBody:     false,
			expectedCode: codes.PermissionDenied,
		},
		// TODO currently ResolveComment and Edit map to the same iam/statuses, so no distinction
		{
			name:         "admin can edit",
			userIdentity: suite.users.Krosh.Identity,
			resolve:      false,
			editBody:     true,
			expectedCode: codes.OK,
		},
		{
			name:         "admin can resolve",
			userIdentity: suite.users.Krosh.Identity,
			resolve:      true,
			editBody:     false,
			expectedCode: codes.OK,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &pb.UpdateCommentRequest{
				PrId:       pbPrID,
				CommentId:  grpc2.MarshalID(comment.ID),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{}},
			}
			if test.editBody {
				request.Body = utils.PtrFromValue("Edited")
				request.UpdateMask.Paths = append(request.UpdateMask.Paths, "body")
			}
			if test.resolve {
				request.IsResolved = utils.PtrFromValue(true)
				request.UpdateMask.Paths = append(request.UpdateMask.Paths, "is_resolved")
			}

			_, err := client.Update(testutils.AuthorizeGRPC(test.userIdentity), request)
			yarequire.ProtoStatusEqual(t, test.expectedCode, err)
		})
	}
}

func (suite *RwApiTestSuite) TestComments_Publish() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	ctx := testutils.AuthorizeGRPC(user.Identity)

	pr := suite.makePullRequest(user, nil)
	pbPrID := grpc2.MarshalID(pr.ID)

	feed1 := suite.makeSmallPrFeed(user, pr)
	feed2 := suite.makeSmallPrFeed(user, pr)

	t.Run("publish ok", func(t *testing.T) {
		op, err := client.PublishDrafts(ctx, &pb.PublishDraftsRequest{PrId: pbPrID})
		require.NoError(t, err)

		response, err := op.GetResponse().UnmarshalNew()
		require.NoError(t, err)
		pub, ok := response.(*pb.PublishDraftsResponse)
		require.True(t, ok)

		require.Equal(t, pub.PublishedCount, uint64(2))
	})

	t.Run("comments are published", func(t *testing.T) {
		for _, commentID := range append(feed1[:], feed2...) {
			comment, err := client.Get(ctx, &pb.GetCommentRequest{
				PrId:      pbPrID,
				CommentId: grpc2.MarshalID(commentID),
			})
			require.NoError(t, err)
			require.True(t, *comment.IsPublished)
		}
	})
}

func (suite *RwApiTestSuite) TestComments_CommentType() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	ctx := testutils.AuthorizeGRPC(user.Identity)

	pr := suite.makePullRequest(user, nil)
	pbPrID := grpc2.MarshalID(pr.ID)

	for _, test := range []struct {
		name         string
		commentType  pb.PRCommentType
		expectedType pb.PRCommentType
	}{
		{
			name:         "explicit default type",
			commentType:  pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			expectedType: pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
		},
		{
			name:         "explicit unspecified type should be default",
			commentType:  pb.PRCommentType_PR_COMMENT_TYPE_UNSPECIFIED,
			expectedType: pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
		},
		{
			name:         "appsec comment",
			commentType:  pb.PRCommentType_PR_COMMENT_TYPE_APPSEC,
			expectedType: pb.PRCommentType_PR_COMMENT_TYPE_APPSEC,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			op, err := client.Create(ctx, &pb.CreateCommentRequest{
				PrId:    pbPrID,
				Body:    "Test comment with type",
				Publish: true,
				Type:    test.commentType,
			})
			require.NoError(t, err)

			comment, err := op.GetResponse().UnmarshalNew()
			require.NoError(t, err)
			cmt, ok := comment.(*pb.PRComment)
			require.True(t, ok)

			// Verify the type is saved correctly
			require.Equal(t, test.expectedType, cmt.Type)

			// Get the comment again to ensure it's persisted
			cmtGet, err := client.Get(ctx, &pb.GetCommentRequest{
				PrId:      pbPrID,
				CommentId: cmt.Id,
			})
			require.NoError(t, err)
			require.Equal(t, test.expectedType, cmtGet.Type)
		})
	}
}

func (suite *RwApiTestSuite) TestComments_CreateBulk() {
	t := suite.T()
	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	ctx := testutils.AuthorizeGRPC(user.Identity)

	suite.addRole(t, user, suite.repos.PrValidation, iam.Roles.RepositoriesDeveloper)
	pr := suite.makePullRequest(user, &makePrOptions{
		Repo:   suite.repos.PrValidation,
		Source: "validate-2",
		Target: "validate-1",
	})

	suite.mustBash(suite.repos.PrValidation, `
			git checkout validate-2
			echo something >> file1.txt
			git add .
			git commit -m "Something for anchor test"
		`)
	pbPrID := grpc2.MarshalID(pr.ID)

	// Create parent comment for threading tests
	parent := suite.makePrCommentGRPC(t, user, pr, &makePrCommentOptions{Body: "Parent comment", Draft: false})
	pbParentID := grpc2.MarshalID(parent.ID)

	t.Run("create bulk comments with order preservation", func(t *testing.T) {
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:           "First comment",
				Publish:        true,
				Type:           pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				NeedResolution: false,
			},
			{
				Body:           "Second comment with parent",
				Publish:        true,
				ParentId:       &pbParentID,
				Type:           pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				NeedResolution: true,
			},
			{
				Body:           "Third comment as draft",
				Publish:        false,
				Type:           pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				NeedResolution: false,
			},
			{
				Body:           "Fourth comment AppSec type",
				Publish:        true,
				Type:           pb.PRCommentType_PR_COMMENT_TYPE_APPSEC,
				NeedResolution: false,
			},
		}

		op, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:                pbPrID,
			Comments:            comments,
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		response, err := op.GetResponse().UnmarshalNew()
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(
			t, response,
			protocmp.IgnoreFields(&pb.PRComment{}, "created_at", "updated_at", "iteration", "id", "parent_id"),
			protocmp.IgnoreFields(&operation.Operation{}, "id"),
		)
	})

	t.Run("empty comments list", func(t *testing.T) {
		_, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:                pbPrID,
			Comments:            []*pb.CreateBulkCommentRequest_CreateCommentData{},
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "comments: value is required")
	})

	t.Run("too many comments", func(t *testing.T) {
		comments := make([]*pb.CreateBulkCommentRequest_CreateCommentData, 101) // Over limit of 100
		for i := 0; i < 101; i++ {
			comments[i] = &pb.CreateBulkCommentRequest_CreateCommentData{
				Body:    "Comment " + grpc2.MarshalID(uint64(i+1)),
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			}
		}

		_, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:     pbPrID,
			Comments: comments,
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})

	t.Run("invalid parent comment", func(t *testing.T) {
		nonExistentParentID := "999999"
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:     "Comment with invalid parent",
				Publish:  true,
				ParentId: &nonExistentParentID,
				Type:     pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		}

		_, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:     pbPrID,
			Comments: comments,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("mixed published and draft comments", func(t *testing.T) {
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:    "Published comment 1",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
			{
				Body:    "Draft comment 1",
				Publish: false,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
			{
				Body:    "Published comment 2",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_APPSEC,
			},
		}

		op, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:                pbPrID,
			Comments:            comments,
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, op)
		yarequire.ProtoCompareWithFixture(
			t, op,
			protocmp.IgnoreFields(&pb.PRComment{}, "created_at", "updated_at", "iteration", "id"),
			protocmp.IgnoreFields(&operation.Operation{}, "id"),
		)
	})

	t.Run("permission denied for non-contributor", func(t *testing.T) {
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:    "Unauthorized comment",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		}

		_, err := client.CreateBulk(context.Background(), &pb.CreateBulkCommentRequest{
			PrId:     pbPrID,
			Comments: comments,
		})
		yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
	})

	t.Run("validation errors", func(t *testing.T) {
		// Empty body should fail
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:    "",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
			},
		}

		_, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:     pbPrID,
			Comments: comments,
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})

	t.Run("create bulk comments with anchors", func(t *testing.T) {
		comments := []*pb.CreateBulkCommentRequest_CreateCommentData{
			{
				Body:    "Comment with file anchor",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				Anchor: &pb.CreateBulkCommentRequest_ShortAnchor{
					Path: "file1.txt",
					Position: &pb.DiffPos{
						From: 1,
						To:   2,
						Side: pb.DiffPos_SOURCE,
					},
				},
				NeedResolution: true,
			},
			{
				Body:    "Comment with line anchor",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				Anchor: &pb.CreateBulkCommentRequest_ShortAnchor{
					Path: "file1.txt",
					Position: &pb.DiffPos{
						From: 2,
						To:   2,
						Side: pb.DiffPos_SOURCE,
					},
				},
			},
			{
				Body:    "Comment with range anchor",
				Publish: true,
				Type:    pb.PRCommentType_PR_COMMENT_TYPE_DEFAULT,
				Anchor: &pb.CreateBulkCommentRequest_ShortAnchor{
					Path: "file1.txt",
					Position: &pb.DiffPos{
						From: 1,
						To:   1,
						Side: pb.DiffPos_TARGET,
					},
				},
			},
		}

		op, err := client.CreateBulk(ctx, &pb.CreateBulkCommentRequest{
			PrId:                pbPrID,
			Comments:            comments,
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		response, err := op.GetResponse().UnmarshalNew()
		require.NoError(t, err)

		bulkResponse := response.(*pb.CreateBulkCommentResponse)
		require.Len(t, bulkResponse.Comments, 3)

		//yarequire.ProtoDumpFixture(t, op)
		yarequire.ProtoCompareWithFixture(
			t, op,
			protocmp.IgnoreFields(&pb.PRComment{}, "created_at", "updated_at", "iteration", "id"),
			protocmp.IgnoreFields(&operation.Operation{}, "id"),
		)
	})
}
