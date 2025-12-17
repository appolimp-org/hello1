package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/revision"
	"gitcore/internal/testutils"
	"gitcore/pkg/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestIssueAttachments() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	kroshIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Krosh issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
	catID := attachment.ID
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueAttachment)
	rabbitID := attachment.ID

	// check db state
	catIDd, err := grpc_marshalling.IDDirect(catID)
	require.NoError(t, err)
	rabbitIDd, err := grpc_marshalling.IDDirect(rabbitID)
	require.NoError(t, err)

	catAttach, err := suite.AttachmentRepo.Get(ctx, catIDd)
	require.NoError(t, err)
	require.Equal(t, entities.AttachmentFileTypes.Image, catAttach.FileType)
	require.Greater(t, catAttach.Size, int64(0))
	require.Equal(t, entities.AttachmentStatuses.Uploaded, catAttach.Status)
	require.False(t, catAttach.IsDeleted)

	rabbitAttach, err := suite.AttachmentRepo.Get(ctx, rabbitIDd)
	require.NoError(t, err)
	require.Equal(t, entities.AttachmentFileTypes.Image, rabbitAttach.FileType)
	require.Greater(t, rabbitAttach.Size, int64(0))
	require.Equal(t, entities.AttachmentStatuses.Uploaded, catAttach.Status)
	require.False(t, rabbitAttach.IsDeleted)

	// can not attach Kopatych tmp attachments to Krosh issue
	req := &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(kroshIssue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catID,
				Action: pb.DeltaAction_ADD,
			},
			{
				Id:     rabbitID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	_, err = client.UpdateAttachments(kroshCtx, req)
	require.Error(t, err)
	yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)

	// can attach Kopatych tmp attachments to Kopatych issue
	req = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catID,
				Action: pb.DeltaAction_ADD,
			},
			{
				Id:     rabbitID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	op, err := client.UpdateAttachments(ctx, req)
	require.NoError(t, err)

	pbUpdatedIssue, err := grpc_marshalling.OperationResponse(op, &pb.Issue{})
	require.NoError(t, err)
	require.Equal(t, 2, len(pbUpdatedIssue.AttachmentsIds))
	require.Equal(t, catID, pbUpdatedIssue.AttachmentsIds[0])
	require.Equal(t, rabbitID, pbUpdatedIssue.AttachmentsIds[1])

	// rm one of them
	req = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catID,
				Action: pb.DeltaAction_REMOVE,
			},
		},
	}

	op, err = client.UpdateAttachments(ctx, req)
	require.NoError(t, err)

	pbUpdatedIssue, err = grpc_marshalling.OperationResponse(op, &pb.Issue{})
	require.NoError(t, err)
	require.Equal(t, 1, len(pbUpdatedIssue.AttachmentsIds))
	require.Equal(t, rabbitID, pbUpdatedIssue.AttachmentsIds[0])

	// check db state for deleted
	attach, err := suite.AttachmentRepo.Get(ctx, catIDd)
	require.NoError(t, err)
	require.Equal(t, entities.AttachmentStatuses.Attached, attach.Status)
	require.True(t, attach.IsDeleted)

	// check events
	issueEventsAll, err := suite.IssueFeedRepo.ListEventsByIssue(ctx, issue.ID, pagination.Options{})
	require.NoError(t, err)
	attachmentEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.IssueAttachmentsUpdated
	})
	require.Len(t, attachmentEvents, 2)
	require.Len(t, attachmentEvents[0].Payload.Attachments.Added, 2)
	require.Contains(t, attachmentEvents[0].Payload.Attachments.Added, catIDd)
	require.Contains(t, attachmentEvents[0].Payload.Attachments.Added, rabbitIDd)
	require.Empty(t, attachmentEvents[0].Payload.Attachments.Removed)

	require.Empty(t, attachmentEvents[1].Payload.Attachments.Added)
	require.Len(t, attachmentEvents[1].Payload.Attachments.Removed, 1)
	require.Contains(t, attachmentEvents[1].Payload.Attachments.Removed, catIDd)
}

func (suite *RwApiTestSuite) TestIssueCommentAttachments() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	comment := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "A comment",
	})

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "document.pdf", entities.AttachmentScopes.IssueCommentAttachment)
	catRabbitID := attachment.ID

	// check db state

	catRabbitIDd, err := grpc_marshalling.IDDirect(catRabbitID)
	require.NoError(t, err)
	attach, err := suite.AttachmentRepo.Get(ctx, catRabbitIDd)

	require.NoError(t, err)
	require.Equal(t, entities.AttachmentStatuses.Uploaded, attach.Status)
	require.Equal(t, false, attach.IsDeleted)
	require.Equal(t, entities.AttachmentFileTypes.Document, attach.FileType)
	require.Equal(t, "document.pdf", attach.Name)

	// add new attachment

	req := &pb.UpdateIssueCommentAttachmentsRequest{
		CommentId: grpc_marshalling.IDInverse(comment.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catRabbitID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	op, err := client.UpdateAttachments(ctx, req)
	require.NoError(t, err)

	pbUpdatedComment, err := grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
	require.NoError(t, err)

	require.Equal(t, 1, len(pbUpdatedComment.AttachmentsIds))
	require.Equal(t, catRabbitID, pbUpdatedComment.AttachmentsIds[0])

	// rm it
	req = &pb.UpdateIssueCommentAttachmentsRequest{
		CommentId: grpc_marshalling.IDInverse(comment.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catRabbitID,
				Action: pb.DeltaAction_REMOVE,
			},
		},
	}
	op, err = client.UpdateAttachments(ctx, req)
	require.NoError(t, err)
	pbUpdatedComment, err = grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
	require.NoError(t, err)
	require.Equal(t, 0, len(pbUpdatedComment.AttachmentsIds))

	// check db state
	rabbitIDd, err := grpc_marshalling.IDDirect(catRabbitID)
	require.NoError(t, err)
	attach, err = suite.AttachmentRepo.Get(ctx, rabbitIDd)
	require.NoError(t, err)
	require.Equal(t, entities.AttachmentStatuses.Attached, attach.Status)
	require.True(t, attach.IsDeleted)

	// check events
	issueEventsAll, err := suite.IssueFeedRepo.ListEventsByIssue(ctx, issue.ID, pagination.Options{})
	require.NoError(t, err)
	attachmentEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.CommentAttachmentsUpdated
	})
	require.Len(t, attachmentEvents, 2)
	require.Len(t, attachmentEvents[0].Payload.Attachments.Added, 1)
	require.Contains(t, attachmentEvents[0].Payload.Attachments.Added, catRabbitIDd)
	require.Empty(t, attachmentEvents[0].Payload.Attachments.Removed)

	require.Empty(t, attachmentEvents[1].Payload.Attachments.Added)
	require.Len(t, attachmentEvents[1].Payload.Attachments.Removed, 1)
	require.Contains(t, attachmentEvents[1].Payload.Attachments.Removed, catRabbitIDd)
}

func (suite *RwApiTestSuite) TestIssueAndCommentAttachments() {
	t := suite.T()
	isseuCommentclient := pb.NewIssueCommentServiceClient(suite.grpcClient)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	noAttachIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Another Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	comment1 := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "A comment",
	})

	comment2 := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{
		IssueID: &issue.ID,
		Body:    "Another comment",
	})

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
	catID := attachment.ID
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueCommentAttachment)
	rabbitID := attachment.ID
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "document.pdf", entities.AttachmentScopes.IssueAttachment)
	catRabbitID := attachment.ID
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "document.docx", entities.AttachmentScopes.IssueAttachment)
	documentID := attachment.ID

	req := &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catRabbitID,
				Action: pb.DeltaAction_ADD,
			},
			{
				Id:     documentID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	_, err := issueClient.UpdateAttachments(ctx, req)
	require.NoError(t, err)

	cmtReq := &pb.UpdateIssueCommentAttachmentsRequest{
		CommentId: grpc_marshalling.IDInverse(comment1.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     catID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}
	_, err = isseuCommentclient.UpdateAttachments(ctx, cmtReq)
	require.ErrorContains(t, err, "does not belong to the current scope")

	cmtReq = &pb.UpdateIssueCommentAttachmentsRequest{
		CommentId: grpc_marshalling.IDInverse(comment2.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     rabbitID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}
	_, err = isseuCommentclient.UpdateAttachments(ctx, cmtReq)
	require.NoError(t, err)

	listReq := &pb.ListIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
	}
	noAttachListReq := &pb.ListIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(noAttachIssue.ID),
	}

	attachments, err := issueClient.ListAttachments(ctx, listReq)
	require.NoError(t, err)

	require.Equal(t, 3, len(attachments.Attachments))

	// count attachments
	count, err := suite.RevSyncer.ComputeCount(ctx, revision.Issue(issue.ID).Attachments)
	require.NoError(t, err)
	require.Equal(t, 3, count)

	attachments, err = issueClient.ListAttachments(ctx, noAttachListReq)
	require.NoError(t, err)
	require.Equal(t, 0, len(attachments.Attachments))

	// count attachments
	count, err = suite.RevSyncer.ComputeCount(ctx, revision.Issue(noAttachIssue.ID).Attachments)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func (suite *RwApiTestSuite) TestAttachmentsErrors() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	// attach unexisting
	req := &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     "387692",
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	_, err := client.UpdateAttachments(ctx, req)
	yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	require.True(t, strings.Contains(err.Error(), "attachment 387692 does not exist"))

	// attach attached
	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
	attachID := attachment.ID
	req = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     attachID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}

	_, err = client.UpdateAttachments(ctx, req)
	require.NoError(t, err)
	_, err = client.UpdateAttachments(ctx, req)
	yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	require.True(t, strings.Contains(err.Error(), fmt.Sprintf("attachment %s is already attached", attachID)))

	// delete attachment
	delReq := &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     attachID,
				Action: pb.DeltaAction_REMOVE,
			},
		},
	}
	_, err = client.UpdateAttachments(ctx, delReq)
	require.NoError(t, err)
	_, err = client.UpdateAttachments(ctx, req)
	yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	require.True(t, strings.Contains(err.Error(), fmt.Sprintf("attachment %s was deleted", attachID)))

	// delete detached
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueAttachment)
	attachID = attachment.ID
	delReq = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     attachID,
				Action: pb.DeltaAction_REMOVE,
			},
		},
	}
	_, err = client.UpdateAttachments(ctx, delReq)
	yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	require.True(t, strings.Contains(err.Error(), fmt.Sprintf("attachment %s is detached", attachID)))

	// delete attachment from different issue
	issue2 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The 2nd Issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	req = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue2.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     attachID,
				Action: pb.DeltaAction_ADD,
			},
		},
	}
	_, err = client.UpdateAttachments(ctx, req) // add attachment to issue2
	require.NoError(t, err)

	delReq = &pb.UpdateIssueAttachmentsRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
		AttachDeltas: []*pb.IdDelta{
			{
				Id:     attachID,
				Action: pb.DeltaAction_REMOVE,
			},
		},
	}
	_, err = client.UpdateAttachments(ctx, delReq) // try to delete attachment from issue
	yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	require.True(t, strings.Contains(err.Error(), fmt.Sprintf("attachment %s does not belong to the current entity", attachID)))

}

func (suite *RwApiTestSuite) TestGetAttachment() {
	t := suite.T()
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	privateIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Private Issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	testCases := map[string]struct {
		issue            *entities.Issue
		checkAttachments func(t *testing.T, catID string, rabbitID string, pdfID string, documentID string)
	}{
		"public issue": {
			issue: issue,
			checkAttachments: func(t *testing.T, catID string, rabbitID string, pdfID string, documentID string) {
				// issue author (kopatych)
				resp := suite.GetAttachmentUpload(suite.users.Kopatych.Identity, catID, "cat.jpeg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, rabbitID, "rabbit.jpg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, pdfID, "document.pdf")
				require.Equal(t, 200, resp.StatusCode()) // detached, but author can still get it
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, documentID, "document.docx")
				require.Equal(t, 200, resp.StatusCode())

				// another user (krosh)
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, catID, "cat.jpeg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, rabbitID, "rabbit.jpg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, pdfID, "document.pdf")
				require.Equal(t, 403, resp.StatusCode()) // detached attach
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, documentID, "document.docx")
				require.Equal(t, 200, resp.StatusCode())
			},
		},
		"private issue": {
			issue: privateIssue,
			checkAttachments: func(t *testing.T, catID string, rabbitID string, pdfID string, documentID string) {
				// issue author (kopatych)
				resp := suite.GetAttachmentUpload(suite.users.Kopatych.Identity, catID, "cat.jpeg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, rabbitID, "rabbit.jpg")
				require.Equal(t, 200, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, pdfID, "document.pdf")
				require.Equal(t, 200, resp.StatusCode()) // detached, but author can still get it
				resp = suite.GetAttachmentUpload(suite.users.Kopatych.Identity, documentID, "document.docx")
				require.Equal(t, 200, resp.StatusCode())

				// another user (krosh) - forbidden to get attach in private issue
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, catID, "cat.jpeg")
				require.Equal(t, 403, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, rabbitID, "rabbit.jpg")
				require.Equal(t, 403, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, pdfID, "document.pdf")
				require.Equal(t, 403, resp.StatusCode())
				resp = suite.GetAttachmentUpload(suite.users.Krosh.Identity, documentID, "document.docx")
				require.Equal(t, 403, resp.StatusCode())
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
			catID := attachment.ID
			attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueAttachment)
			rabbitID := attachment.ID
			attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "document.pdf", entities.AttachmentScopes.IssueAttachment)
			pdfID := attachment.ID
			attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "document.docx", entities.AttachmentScopes.IssueAttachment)
			documentID := attachment.ID

			req := &pb.UpdateIssueAttachmentsRequest{
				Id: grpc_marshalling.IDInverse(tc.issue.ID),
				AttachDeltas: []*pb.IdDelta{
					{
						Id:     catID,
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     rabbitID,
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     documentID,
						Action: pb.DeltaAction_ADD,
					},
				},
			}
			_, err := issueClient.UpdateAttachments(ctx, req)
			require.NoError(t, err)

			tc.checkAttachments(t, catID, rabbitID, pdfID, documentID)
		})
	}
}

func (suite *RwApiTestSuite) TestIssueAttachmentsWhileCreate() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
	catID := attachment.ID
	catIDd, err := grpc_marshalling.IDDirect(catID)
	require.NoError(t, err)
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueAttachment)
	rabbitID := attachment.ID
	rabbitIDd, err := grpc_marshalling.IDDirect(rabbitID)
	require.NoError(t, err)

	request := &pb.CreateIssueRequest{
		RepoId:        grpc_marshalling.IDInverse(repoID),
		Title:         "Simple issue",
		AttachmentIds: []string{catID, rabbitID},
		Visibility:    pb.Issue_VISIBILITY_PUBLIC,
	}

	op, err := client.Create(ctx, request)
	require.NoError(t, err)
	pbIssue, err := grpc_marshalling.OperationResponse(op, &pb.Issue{})
	require.NoError(t, err)
	require.Equal(t, 2, len(pbIssue.AttachmentsIds))
	require.Contains(t, pbIssue.AttachmentsIds, catID)
	require.Contains(t, pbIssue.AttachmentsIds, rabbitID)

	// check events
	issueID, err := grpc_marshalling.IDDirect(pbIssue.Id)
	require.NoError(t, err)
	issueEventsAll, err := suite.IssueFeedRepo.ListEventsByIssue(ctx, issueID, pagination.Options{})
	require.NoError(t, err)
	attachmentEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.IssueAttachmentsUpdated
	})
	issueCreateEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.IssueCreated
	})
	require.Equal(t, issueCreateEvents[0].ChangeID, attachmentEvents[0].ChangeID)
	require.Len(t, attachmentEvents, 1)
	require.Len(t, attachmentEvents[0].Payload.Attachments.Added, 2)
	require.Contains(t, attachmentEvents[0].Payload.Attachments.Added, catIDd)
	require.Contains(t, attachmentEvents[0].Payload.Attachments.Added, rabbitIDd)
	require.Empty(t, attachmentEvents[0].Payload.Attachments.Removed)
}

func (suite *RwApiTestSuite) TestIssueCommentAttachmentsWhileCreate() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueCommentAttachment)
	catID := attachment.ID
	catIDd, err := grpc_marshalling.IDDirect(catID)
	require.NoError(t, err)
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueCommentAttachment)
	rabbitID := attachment.ID
	rabbitIDd, err := grpc_marshalling.IDDirect(rabbitID)
	require.NoError(t, err)

	op, err := client.Create(ctx, &pb.CreateIssueCommentRequest{
		IssueId:       grpc_marshalling.IDInverse(issue.ID),
		Body:          "admin comment",
		AttachmentIds: []string{catID, rabbitID},
	})
	require.NoError(t, err)

	pbComment, err := grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
	require.NoError(t, err)

	require.Equal(t, 2, len(pbComment.AttachmentsIds))
	require.Contains(t, pbComment.AttachmentsIds, catID)
	require.Contains(t, pbComment.AttachmentsIds, rabbitID)

	issueEventsAll, err := suite.IssueFeedRepo.ListEventsByIssue(ctx, issue.ID, pagination.Options{})
	require.NoError(t, err)
	commentAttachmentEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.CommentAttachmentsUpdated
	})
	commentCreateEvents := functools.Filter(issueEventsAll.Result, func(event *entities.IssueEvent) bool {
		return event.EventType == entities.IssueEventTypes.RootCommentCreated
	})
	require.Equal(t, commentCreateEvents[0].ChangeID, commentAttachmentEvents[0].ChangeID)

	require.Len(t, commentAttachmentEvents, 1)
	require.Len(t, commentAttachmentEvents[0].Payload.Attachments.Added, 2)
	require.Contains(t, commentAttachmentEvents[0].Payload.Attachments.Added, catIDd)
	require.Contains(t, commentAttachmentEvents[0].Payload.Attachments.Added, rabbitIDd)
}

func (suite *RwApiTestSuite) TestCleanupAttachments() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	tests := []struct {
		name                        string
		uploadAttachment            func() string
		cleanupTmpAfter             time.Duration
		cleanupDeletedAfter         time.Duration
		expectToDeleted             bool
		expectedStatusBeforeCleanup entities.AttachmentStatus
	}{
		{
			name: "Temporary attachment cleaned",
			uploadAttachment: func() string {
				res := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
				time.Sleep(time.Millisecond)
				return res.ID
			},
			cleanupTmpAfter:             time.Millisecond,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             true,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Uploaded,
		},
		{
			name: "Temporary avatar cleaned",
			uploadAttachment: func() string {
				res := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.UserAvatar)
				time.Sleep(time.Millisecond)
				return res.ID
			},
			cleanupTmpAfter:             time.Millisecond,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             true,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Uploaded,
		},
		{
			name: "Temporary attachment not cleaned",
			uploadAttachment: func() string {
				res := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
				return res.ID
			},
			cleanupTmpAfter:             time.Hour,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             false,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Uploaded,
		},
		{
			name: "Temporary uploading attachment cleaned",
			uploadAttachment: func() string {
				res := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
				attachmentID, err := grpc_marshalling.IDDirect(res.ID)
				require.NoError(t, err)

				// change status to uploading
				require.NoError(t, suite.AttachmentRepo.
					Update(attachmentID, &suite.users.Kopatych.ID).
					SetStatus(entities.AttachmentStatuses.Uploading).
					Commit(ctx))

				time.Sleep(time.Millisecond)
				return res.ID
			},
			cleanupTmpAfter:             time.Millisecond,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             true,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Uploading,
		},
		{
			name: "Temporary uploading attachment not cleaned",
			uploadAttachment: func() string {
				res := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)
				attachmentID, err := grpc_marshalling.IDDirect(res.ID)
				require.NoError(t, err)

				// change status to uploading
				require.NoError(t, suite.AttachmentRepo.
					Update(attachmentID, &suite.users.Kopatych.ID).
					SetStatus(entities.AttachmentStatuses.Uploading).
					Commit(ctx))

				return res.ID
			},
			cleanupTmpAfter:             time.Hour,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             false,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Uploading,
		},
		{
			name: "Deleted attachment cleaned",
			uploadAttachment: func() string {
				attach := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)

				// add attach
				req := &pb.UpdateIssueAttachmentsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					AttachDeltas: []*pb.IdDelta{
						{
							Id:     attach.ID,
							Action: pb.DeltaAction_ADD,
						},
					},
				}
				_, err := issueClient.UpdateAttachments(ctx, req)
				require.NoError(t, err)

				req = &pb.UpdateIssueAttachmentsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					AttachDeltas: []*pb.IdDelta{
						{
							Id:     attach.ID,
							Action: pb.DeltaAction_REMOVE,
						},
					},
				}
				_, err = issueClient.UpdateAttachments(ctx, req)
				require.NoError(t, err)

				time.Sleep(time.Millisecond)
				return attach.ID
			},
			cleanupTmpAfter:             time.Hour,
			cleanupDeletedAfter:         time.Millisecond,
			expectToDeleted:             true,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Attached,
		},
		{
			name: "Deleted attachment not cleaned",
			uploadAttachment: func() string {
				attach := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueAttachment)

				// add attach
				req := &pb.UpdateIssueAttachmentsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					AttachDeltas: []*pb.IdDelta{
						{
							Id:     attach.ID,
							Action: pb.DeltaAction_ADD,
						},
					},
				}
				_, err := issueClient.UpdateAttachments(ctx, req)
				require.NoError(t, err)

				req = &pb.UpdateIssueAttachmentsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					AttachDeltas: []*pb.IdDelta{
						{
							Id:     attach.ID,
							Action: pb.DeltaAction_REMOVE,
						},
					},
				}
				_, err = issueClient.UpdateAttachments(ctx, req)
				require.NoError(t, err)

				return attach.ID
			},
			cleanupTmpAfter:             time.Hour,
			cleanupDeletedAfter:         time.Hour,
			expectToDeleted:             false,
			expectedStatusBeforeCleanup: entities.AttachmentStatuses.Attached,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attachID, err := grpc_marshalling.IDDirect(tc.uploadAttachment())
			require.NoError(t, err)
			attach, err := suite.AttachmentRepo.Get(ctx, attachID)
			require.NoError(t, err)
			require.NotNil(t, attach.S3Key)
			require.Equal(t, attach.Status, tc.expectedStatusBeforeCleanup)

			attachWithKey := &entities.Attachment{
				S3Key: attach.S3Key,
				Scope: attach.Scope,
			}
			_, _, err = suite.UploadService.Get(ctx, attachWithKey)
			require.NoError(t, err)

			err = suite.AttachmentService.CleanupS3Files(ctx, tc.cleanupTmpAfter, tc.cleanupDeletedAfter)
			require.NoError(t, err)

			attach, err = suite.AttachmentRepo.Get(ctx, attachID)
			require.NoError(t, err)
			_, _, err = suite.UploadService.Get(ctx, attachWithKey)
			if tc.expectToDeleted {
				require.Equal(t, entities.AttachmentStatuses.RemovedFromStorage, attach.Status)
				require.Nil(t, attach.S3Key)
				require.Error(t, err)
			} else {
				require.NotEqual(t, entities.AttachmentStatuses.RemovedFromStorage, attach.Status)
				require.NotNil(t, attach.S3Key)
				require.NoError(t, err)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestIssueCommentAttachments_WebSocket() {
	t := suite.T()
	client := pb.NewIssueCommentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "The Issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	attachment := suite.UploadAttachment(suite.users.Kopatych.Identity, "cat.jpeg", entities.AttachmentScopes.IssueCommentAttachment)
	catID := attachment.ID
	attachment = suite.UploadAttachment(suite.users.Kopatych.Identity, "rabbit.jpg", entities.AttachmentScopes.IssueCommentAttachment)
	rabbitID := attachment.ID

	createOp, err := client.Create(ctx, &pb.CreateIssueCommentRequest{
		IssueId:       grpc_marshalling.IDInverse(issue.ID),
		Body:          "admin comment",
		AttachmentIds: []string{catID, rabbitID},
	})
	require.NoError(t, err)
	pbComment, err := grpc_marshalling.OperationResponse(createOp, &pb.IssueComment{})
	require.NoError(t, err)
	commentID := pbComment.Id
	suite.requireHasWsMessage(t, fmt.Sprintf("repository_%d", repoID), fmt.Sprintf(`{
		"identity": {
			"type": "IssueAttachmentsCollection",
			"issueID": "%d"
		}
	}`, issue.ID))

	require.NoError(t, suite.ClearWebSocketRequests())
	_, err = client.Delete(ctx, &pb.DeleteIssueCommentRequest{
		CommentId: commentID,
	})
	require.NoError(t, err)
	suite.requireHasWsMessage(t, fmt.Sprintf("repository_%d", repoID), fmt.Sprintf(`{
		"identity": {
			"type": "IssueAttachmentsCollection",
			"issueID": "%d"
		}
	}`, issue.ID))
}
