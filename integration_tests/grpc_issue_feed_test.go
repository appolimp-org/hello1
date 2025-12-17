package integrationtests

import (
	"common/functools"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"gitcore/pkg/pagination"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestGrpcIssueFeed() {
	t := suite.T()
	prClient := pb.NewPRServiceClient(suite.grpcClient)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	issueLinkClient := pb.NewIssueLinkServiceClient(suite.grpcClient)
	commentClient := pb.NewIssueCommentServiceClient(suite.grpcClient)

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)
	suite.addRole(t, suite.users.Barash, repo, iam.Roles.RepositoriesContributor)
	require.NoError(t, err)

	suite.mustBash(repo, `
		touch file
		git add . && git commit -m "initial"
		git branch -m master
		git checkout -b branch
		touch x
		git add . && git commit -m "something"
	`)

	var issue *entities.Issue
	pairIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "PairIssue",
		Visibility: entities.IssueVisibilities.Public,
	})
	var issueLinkID *uint64
	var invertedIssueLinkID *uint64

	var parentCmtID string
	var childCmtID string
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	kopCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	barCtx := testutils.AuthorizeGRPC(suite.users.Barash.Identity)

	label1 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Grey,
	})
	label2 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Task",
		Color:  entities.PresetLabelColors.Red,
	})

	pr1 := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "branch",
		Target: "master",
	})
	pr2 := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "branch",
		Target: "master",
	})
	pr3 := suite.makePullRequest(suite.users.Kopatych, nil)

	testCases := []struct {
		createEvent   func()
		testDbRecords func([]*entities.IssueEvent)
		testFeed      func([]*pb.IssueFeedItem)
	}{
		{
			createEvent: func() {
				issue = suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
					RepoID:     repoID,
					Title:      "KopIssue",
					LabelIDs:   []uint64{label1.ID},
					Visibility: entities.IssueVisibilities.Public,
				})
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 5)
				for _, event := range events {
					require.Equal(t, issue.ID, event.IssueID)
					require.Equal(t, entities.IssueEventTypes.IssueCreated, event.EventType)
					require.Nil(t, event.CommentID)
					switch event.Field {
					case "title":
						require.Equal(t, "KopIssue", *event.Payload.Change.To)
						require.Nil(t, event.Payload.Change.From)
					case "visibility":
						require.Equal(t, entities.IssueVisibilities.Public, *event.Payload.Change.To)
						require.Nil(t, event.Payload.Change.From)
					case "prioriy":
						require.Equal(t, entities.IssuePriorities.Normal, *event.Payload.Change.To)
						require.Nil(t, event.Payload.Change.From)
					case "status_id":
						require.Equal(t, entities.IssueStatuses.Open.ID, *event.Payload.Change.To)
						require.Nil(t, event.Payload.Change.From)
					case "label_ids":
						require.Equal(t, []uint64{label1.ID}, event.Payload.Labels.Added)
						require.Empty(t, event.Payload.Labels.Removed)
					}
				}
			},
		},
		{
			createEvent: func() {
				_, err = commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId: grpc_marshalling.IDInverse(issue.ID),
					Body:    "just a comment",
				})
				require.NoError(t, err)
			},
			testFeed: func(feedItems []*pb.IssueFeedItem) {
				require.Len(t, feedItems, 1)
				feedItem := feedItems[0]
				evUserID, err := grpc_marshalling.IDDirect(feedItem.UserId)
				require.NoError(t, err)
				require.Equal(t, suite.users.Kopatych.ID, evUserID)
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Nil(t, feedItem.Details.GetDiff())
			},
		},
		{
			createEvent: func() {
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id:    grpc_marshalling.IDInverse(issue.ID),
					Title: "Changed Tile",
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"title"},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Equal(t, issue.ID, event.IssueID)
				require.Equal(t, entities.IssueEventTypes.IssueUpdated, event.EventType)
				require.Nil(t, event.CommentID)
				change := event.Payload.Change
				require.Equal(t, "title", change.FieldName)
				require.Equal(t, "Changed Tile", *change.To)
				require.Equal(t, "KopIssue", *change.From)
			},
		},
		{
			createEvent: func() {
				// miltiple fields in update
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id:          grpc_marshalling.IDInverse(issue.ID),
					Title:       "New Tile",
					StatusId:    grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID),
					AssigneeId:  grpc_marshalling.IDInverse(suite.users.Barash.ID),
					Description: "New Description",
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"title", "status_id", "assignee_id", "description"},
					},
				})
				require.NoError(t, err)
			},
			testFeed: func(feedItems []*pb.IssueFeedItem) {
				require.Len(t, feedItems, 1)
				feedItem := feedItems[0]

				evUserID, err := grpc_marshalling.IDDirect(feedItem.UserId)
				require.NoError(t, err)
				require.Equal(t, suite.users.Kopatych.ID, evUserID)
				require.Equal(t, pb.IssueFeedItem_ISSUE_UPDATED, feedItem.EventType)

				change := feedItem.Details.GetDiff()
				require.Equal(t, "status_id", change.FieldName)
				toStatusID, err := strconv.ParseInt(change.To, 10, 32)
				require.NoError(t, err)
				require.Equal(t, entities.IssueStatuses.InProgress.ID, int32(toStatusID))
				fromStatusID, err := strconv.ParseInt(change.From, 10, 32)
				require.NoError(t, err)
				require.Equal(t, entities.IssueStatuses.Open.ID, int32(fromStatusID))
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 4)
				for _, event := range events {
					require.Equal(t, issue.ID, event.IssueID)
					require.Equal(t, entities.IssueEventTypes.IssueUpdated, event.EventType)
					require.Nil(t, event.CommentID)
					change := event.Payload.Change
					switch change.FieldName {
					case "title":
						require.Equal(t, "Changed Tile", *change.From)
						require.Equal(t, "New Tile", *change.To)
					case "status_id":
						require.Equal(t, entities.IssueStatuses.Open.ID, *change.From)
						require.Equal(t, entities.IssueStatuses.InProgress.ID, *change.To)
					case "assignee_id":
						require.Nil(t, change.From)
						require.Equal(t, suite.users.Barash.ID, *change.To)
					case "description":
						require.Equal(t, "New Description", *change.To)
						require.Equal(t, "", *change.From)
					default:
						// TODO Как оно падает читаемо?
						require.Fail(t, "unknown field name")
					}
				}
			},
		},
		{
			createEvent: func() {
				// another comment
				op, err := commentClient.Create(barCtx, &pb.CreateIssueCommentRequest{
					IssueId: grpc_marshalling.IDInverse(issue.ID),
					Body:    "just a comment",
				})
				require.NoError(t, err)
				comment, err := grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
				parentCmtID = comment.Id
			},
			testFeed: func(feedItems []*pb.IssueFeedItem) {
				require.Len(t, feedItems, 1)
				feedItem := feedItems[0]
				evUserID, err := grpc_marshalling.IDDirect(feedItem.UserId)
				require.NoError(t, err)
				require.Equal(t, suite.users.Barash.ID, evUserID)
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Nil(t, feedItem.Details.GetDiff())
			},
		},
		{
			createEvent: func() {
				op, err := commentClient.Create(barCtx, &pb.CreateIssueCommentRequest{
					IssueId:  grpc_marshalling.IDInverse(issue.ID),
					Body:     "reply a comment",
					ParentId: &parentCmtID,
				})
				require.NoError(t, err)
				comment, err := grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
				childCmtID = comment.Id
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Equal(t, entities.IssueEventTypes.CommentCreated, event.EventType)
				require.NotNil(t, event.CommentID)
				require.Equal(t, childCmtID, grpc_marshalling.IDInverse(*event.CommentID))
				require.Nil(t, event.Payload.Change)
				bodyEdit := event.Payload.Comment
				require.Equal(t, "reply a comment", *bodyEdit.NewBody)
				require.Nil(t, bodyEdit.OldBody)
			},
		},
		{
			createEvent: func() {
				_, err = commentClient.Delete(barCtx, &pb.DeleteIssueCommentRequest{
					CommentId: childCmtID,
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Equal(t, entities.IssueEventTypes.CommentDeleted, event.EventType)
			},
		},
		{
			createEvent: func() {
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"assignee_id"},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				change := event.Payload.Change
				require.Equal(t, entities.IssueEventTypes.IssueUpdated, event.EventType)
				require.Equal(t, suite.users.Barash.ID, *change.From)
				require.Nil(t, change.To)
			},
		},
		{
			createEvent: func() {
				_, err = issueClient.UpdateLabels(kopCtx, &pb.UpdateIssueLabelsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					LabelDeltas: []*pb.IdDelta{
						{
							Id:     grpc_marshalling.IDInverse(label2.ID),
							Action: pb.DeltaAction_ADD,
						},
						{
							Id:     grpc_marshalling.IDInverse(label1.ID),
							Action: pb.DeltaAction_REMOVE,
						},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueUpdated, event.EventType)
				require.Equal(t, []uint64{label2.ID}, event.Payload.Labels.Added)
				require.Equal(t, []uint64{label1.ID}, event.Payload.Labels.Removed)
			},
		},
		{
			createEvent: func() {
				_, err = issueClient.UpdateLinkedPullRequests(kopCtx, &pb.UpdateLinkedPullRequestsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					PrDeltas: []*pb.IdDelta{
						{
							Id:     grpc_marshalling.IDInverse(pr2.ID),
							Action: pb.DeltaAction_ADD,
						},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinkedPRsUpdated, event.EventType)
				require.Equal(t, []uint64{pr2.ID}, event.Payload.LinkedPRs.Added)
				require.Equal(t, []uint64{}, event.Payload.LinkedPRs.Removed)
			},
		},
		{
			createEvent: func() {
				_, err = issueClient.UpdateLinkedPullRequests(kopCtx, &pb.UpdateLinkedPullRequestsRequest{
					Id: grpc_marshalling.IDInverse(issue.ID),
					PrDeltas: []*pb.IdDelta{
						{
							Id:     grpc_marshalling.IDInverse(pr2.ID),
							Action: pb.DeltaAction_REMOVE,
						},
						{
							Id:     grpc_marshalling.IDInverse(pr1.ID),
							Action: pb.DeltaAction_ADD,
						},
						{
							Id:     grpc_marshalling.IDInverse(pr3.ID),
							Action: pb.DeltaAction_ADD,
						},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinkedPRsUpdated, event.EventType)
				require.Equal(t, []uint64{pr1.ID}, event.Payload.LinkedPRs.Added)
				require.Equal(t, []uint64{pr2.ID}, event.Payload.LinkedPRs.Removed)
			},
		},
		{
			createEvent: func() {
				_, err = prClient.UpdateLinkedIssues(adminCtx, &pb.UpdateLinkedIssuesRequest{
					Id: grpc_marshalling.IDInverse(pr2.ID),
					IssueDeltas: []*pb.IdDelta{
						{
							Id:     grpc_marshalling.IDInverse(issue.ID),
							Action: pb.DeltaAction_ADD,
						},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinkedPRsUpdated, event.EventType)
				require.Equal(t, []uint64{pr2.ID}, event.Payload.LinkedPRs.Added)
				require.Equal(t, []uint64{}, event.Payload.LinkedPRs.Removed)
			},
		},
		{
			createEvent: func() {
				_, err = prClient.UpdateLinkedIssues(adminCtx, &pb.UpdateLinkedIssuesRequest{
					Id: grpc_marshalling.IDInverse(pr1.ID),
					IssueDeltas: []*pb.IdDelta{
						{
							Id:     grpc_marshalling.IDInverse(issue.ID),
							Action: pb.DeltaAction_REMOVE,
						},
					},
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinkedPRsUpdated, event.EventType)
				require.Equal(t, []uint64{}, event.Payload.LinkedPRs.Added)
				require.Equal(t, []uint64{pr1.ID}, event.Payload.LinkedPRs.Removed)
			},
		},
		{
			// create link: issue --relates--> pairIssue
			createEvent: func() {
				resp, err := issueLinkClient.Create(adminCtx, &pb.CreateIssueLinkRequest{
					LeftIssueId:  grpc_marshalling.IDInverse(issue.ID),
					RightIssueId: grpc_marshalling.IDInverse(pairIssue.ID),
					LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
				})
				require.NoError(t, err)
				createdLink, err := grpc_marshalling.OperationResponse(resp, &pb.IssueLink{})
				require.NoError(t, err)
				linkID, _ := grpc_marshalling.IDDirect(createdLink.Id)
				require.NoError(t, err)
				issueLinkID = &linkID
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinksUpdated, event.EventType)
				require.Equal(t, []entities.IssueLinkRef{{
					ID:           *issueLinkID,
					RightIssueID: pairIssue.ID,
					LinkType:     entities.IssueLinkTypes.Relates,
				}}, event.Payload.IssueLinks.Added)
				require.Equal(t, []entities.IssueLinkRef{}, event.Payload.IssueLinks.Removed)
			},
		},
		{
			// delete link: issue --relates--> pairIssue
			createEvent: func() {
				_, err = issueLinkClient.Delete(adminCtx, &pb.DeleteIssueLinkRequest{
					Id: grpc_marshalling.IDInverse(*issueLinkID),
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinksUpdated, event.EventType)
				require.Equal(t, []entities.IssueLinkRef{}, event.Payload.IssueLinks.Added)
				require.Equal(t, []entities.IssueLinkRef{{
					ID:           *issueLinkID,
					RightIssueID: pairIssue.ID,
					LinkType:     entities.IssueLinkTypes.Relates,
				}}, event.Payload.IssueLinks.Removed)
			},
		},
		{
			// create link: pairIssue --relates--> issue
			createEvent: func() {
				resp, err := issueLinkClient.Create(adminCtx, &pb.CreateIssueLinkRequest{
					LeftIssueId:  grpc_marshalling.IDInverse(pairIssue.ID),
					RightIssueId: grpc_marshalling.IDInverse(issue.ID),
					LinkType:     pb.IssueLink_LINK_TYPE_RELATES,
				})
				require.NoError(t, err)
				createdLink, err := grpc_marshalling.OperationResponse(resp, &pb.IssueLink{})
				require.NoError(t, err)
				linkID, _ := grpc_marshalling.IDDirect(createdLink.Id)
				require.NoError(t, err)
				invertedIssueLinkID = &linkID
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinksUpdated, event.EventType)
				require.Equal(t, []entities.IssueLinkRef{{
					ID:           *invertedIssueLinkID,
					RightIssueID: pairIssue.ID,
					LinkType:     entities.IssueLinkTypes.Relates,
				}}, event.Payload.IssueLinks.Added)
				require.Equal(t, []entities.IssueLinkRef{}, event.Payload.IssueLinks.Removed)
			},
		},
		{
			// delete link: pairIssue --relates--> issue
			createEvent: func() {
				_, err = issueLinkClient.Delete(adminCtx, &pb.DeleteIssueLinkRequest{
					Id: grpc_marshalling.IDInverse(*invertedIssueLinkID),
				})
				require.NoError(t, err)
			},
			testDbRecords: func(events []*entities.IssueEvent) {
				require.Len(t, events, 1)
				event := events[0]
				require.Nil(t, event.Payload.Change)
				require.Equal(t, entities.IssueEventTypes.IssueLinksUpdated, event.EventType)
				require.Equal(t, []entities.IssueLinkRef{}, event.Payload.IssueLinks.Added)
				require.Equal(t, []entities.IssueLinkRef{{
					ID:           *invertedIssueLinkID,
					RightIssueID: pairIssue.ID,
					LinkType:     entities.IssueLinkTypes.Relates,
				}}, event.Payload.IssueLinks.Removed)
			},
		},
	}

	for _, tc := range testCases {
		tc.createEvent()
	}

	feedResp, err := issueClient.ListFeed(kopCtx, &pb.ListIssueFeedRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
	})
	require.NoError(t, err)

	issueEventsAll, err := suite.IssueFeedRepo.ListEventsByIssue(kopCtx, issue.ID, pagination.Options{})
	changeIDs := functools.Unique(functools.Map(issueEventsAll.Result, func(event *entities.IssueEvent) uint64 { return event.ChangeID }))
	changeToEvents := functools.GroupBy(issueEventsAll.Result, func(event *entities.IssueEvent) uint64 { return event.ChangeID })
	feedPtr := 0

	for i, tc := range testCases {
		t.Logf("Test case no. %d", i+1)

		if tc.testFeed != nil {
			tc.testFeed(feedResp.EventsByChanges[feedPtr].Events)
			feedPtr++
		}

		if tc.testDbRecords != nil {
			tc.testDbRecords(changeToEvents[changeIDs[i]])
		}
	}

	// delete issue
	_, err = issueClient.Delete(kopCtx, &pb.DeleteIssueRequest{
		Id: grpc_marshalling.IDInverse(issue.ID),
	})
	require.NoError(t, err)

	issueEventsAll, err = suite.IssueFeedRepo.ListEventsByIssue(kopCtx, issue.ID, pagination.Options{})
	require.NoError(t, err)

	deletedEvent := issueEventsAll.Result[len(issueEventsAll.Result)-1]
	require.Equal(t, entities.IssueEventTypes.IssueDeleted, deletedEvent.EventType)
	require.Equal(t, entities.PayloadData{}, deletedEvent.Payload)
}

func (suite *RwApiTestSuite) TestGrpcIssueFeedPagination() {
	t := suite.T()
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	commentClient := pb.NewIssueCommentServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)
	suite.addRole(t, suite.users.Barash, repo, iam.Roles.RepositoriesContributor)
	require.NoError(t, err)
	kopCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	var issue *pb.Issue
	var comment1, comment2, comment3, comment4 *pb.IssueComment

	type testCase struct {
		createEvent func()
		checkEvent  func(*pb.IssueFeedItem)
	}
	testCases := []testCase{
		{
			createEvent: func() {
				// create issue - 0 events in feed
				op, err := issueClient.Create(kopCtx, &pb.CreateIssueRequest{
					RepoId:     grpc_marshalling.IDInverse(repoID),
					Title:      "Issue",
					Visibility: pb.Issue_VISIBILITY_PUBLIC,
				})
				require.NoError(t, err)
				issue, err = grpc_marshalling.OperationResponse(op, &pb.Issue{})
				require.NoError(t, err)
			},
		},
		{
			createEvent: func() {
				// update issue - 2 events, 1 in feed  (1)
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id:       issue.Id,
					StatusId: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID),
					Title:    "New Title",
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"status_id", "title"},
					},
				})
				require.NoError(t, err)
			},
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				change := feedItem.Details.GetDiff()
				require.Equal(t, pb.IssueFeedItem_ISSUE_UPDATED, feedItem.EventType)
				require.Equal(t, "status_id", change.FieldName)
			},
		},
		{
			createEvent: func() {
				// create comment - 1 event in feed   (2)
				op, err := commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId: issue.Id,
					Body:    "just a comment",
				})
				require.NoError(t, err)
				comment1, err = grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
			},
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Equal(t, comment1.Id, feedItem.Details.GetComment().CommentId)
			},
		},
		{
			createEvent: func() {
				// create child comment - not in feed
				_, err := commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId:  issue.Id,
					Body:     "reply a comment",
					ParentId: &comment1.Id,
				})
				require.NoError(t, err)
			},
		},
		{
			createEvent: func() { // (3)
				op, err := commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId: issue.Id,
					Body:    "snd root cmt",
				})
				require.NoError(t, err)
				comment2, err = grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
			},
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Equal(t, comment2.Id, feedItem.Details.GetComment().CommentId)
			},
		},
		{
			createEvent: func() {
				// (4)
				op, err := commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId: issue.Id,
					Body:    "thrd root cmt",
				})
				require.NoError(t, err)
				comment3, err = grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
			},
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Equal(t, comment3.Id, feedItem.Details.GetComment().CommentId)
			},
		},
		{
			createEvent: func() {
				// (5)
				op, err := commentClient.Create(kopCtx, &pb.CreateIssueCommentRequest{
					IssueId: issue.Id,
					Body:    "frth root cmt",
				})
				require.NoError(t, err)
				comment4, err = grpc_marshalling.OperationResponse(op, &pb.IssueComment{})
				require.NoError(t, err)
			},
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				require.Equal(t, pb.IssueFeedItem_ROOT_COMMENT_CREATED, feedItem.EventType)
				require.Equal(t, comment4.Id, feedItem.Details.GetComment().CommentId)
			},
		},
		{
			createEvent: func() {
				// update issue - 0 in feed
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id:         issue.Id,
					StatusId:   grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID),
					AssigneeId: grpc_marshalling.IDInverse(suite.users.Kopatych.ID),
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"assignee_id"},
					},
				})
				require.NoError(t, err)
			},
		},
		{
			createEvent: func() {
				// update issue - 1 in feed (6)
				_, err = issueClient.Update(kopCtx, &pb.UpdateIssueRequest{
					Id:       issue.Id,
					StatusId: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Closed.ID),
					Title:    "New different title",
					UpdateMask: &fieldmaskpb.FieldMask{
						Paths: []string{"status_id", "title"},
					},
				})
				require.NoError(t, err)
			},
			// createEvent: func
			checkEvent: func(feedItem *pb.IssueFeedItem) {
				change := feedItem.Details.GetDiff()
				require.Equal(t, pb.IssueFeedItem_ISSUE_UPDATED, feedItem.EventType)
				require.Equal(t, "status_id", change.FieldName)
			},
		},
	}

	for _, testCase := range testCases {
		testCase.createEvent()
	}
	eventsChecks := functools.Filter(testCases, func(testCase testCase) bool {
		return testCase.checkEvent != nil
	})

	reqPageSizeFwd := uint64(2)
	reqPageSizeBwd := uint64(3)
	listIssueFeedReq := pb.ListIssueFeedRequest{
		Id:       issue.Id,
		PageSize: &reqPageSizeFwd,
		SortBy: []*pagination_pb.SortOption{
			{
				Column:    "change_id",
				Direction: pagination_pb.SortOption_ASC,
			},
		},
	}
	checkSizeFun := func(feedRes *pb.ListIssueFeedResponse, pageSize uint64) {
		require.Len(t, feedRes.EventsByChanges, int(pageSize))
		for i := 0; i < int(pageSize); i++ {
			require.Len(t, feedRes.EventsByChanges[i].Events, 1)
		}
	}

	// iterate fwd
	var events []*pb.IssueFeedItem = make([]*pb.IssueFeedItem, 0)
	for i := 0; i < 3; i++ {
		feedRes, err := issueClient.ListFeed(kopCtx, &listIssueFeedReq)
		require.NoError(t, err)
		checkSizeFun(feedRes, reqPageSizeFwd)
		for _, groupedEvents := range feedRes.EventsByChanges {
			events = append(events, groupedEvents.Events[0])
		}
		listIssueFeedReq.PageToken = &feedRes.NextPageToken
		if i == 3 {
			require.Nil(t, feedRes.NextPageToken)
		}
	}

	for i, event := range events {
		eventsChecks[i].checkEvent(event)
	}

	// iterate bwd
	listIssueFeedReq.SortBy[0].Direction = pagination_pb.SortOption_DESC
	listIssueFeedReq.PageSize = &reqPageSizeBwd
	listIssueFeedReq.PageToken = nil
	events = make([]*pb.IssueFeedItem, 0)
	for i := 0; i < 2; i++ {
		feedRes, err := issueClient.ListFeed(kopCtx, &listIssueFeedReq)
		require.NoError(t, err)
		checkSizeFun(feedRes, reqPageSizeBwd)
		for _, groupedEvents := range feedRes.EventsByChanges {
			events = append(events, groupedEvents.Events[0])
		}
		listIssueFeedReq.PageToken = &feedRes.NextPageToken
		if i == 2 {
			require.Nil(t, feedRes.NextPageToken)
		}
	}

	testsN := len(eventsChecks)
	for i, event := range events {
		eventsChecks[testsN-(i+1)].checkEvent(event)
	}
}
