package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestGrpcUpdateIssue() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	anotherRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	author := suite.users.Krosh

	milestone1 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:   repoID,
		Name:     "Milestone1",
		Deadline: utils.PtrFromValue(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	milestone2 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:   repoID,
		Name:     "Milestone2",
		Deadline: utils.PtrFromValue(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	milestone3 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:   anotherRepoID,
		Name:     "Milestone3",
		Deadline: utils.PtrFromValue(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
	})

	deadline := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	createDefaultIssue := func() uint64 {
		issue := suite.makeIssue(author, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Default Issue",
			Visibility:  entities.IssueVisibilities.Public,
			AssigneeID:  &suite.users.Kopatych.ID,
			MilestoneID: &milestone1.ID,
		})
		return issue.ID
	}

	testCases := map[string]struct {
		user           *entities.User
		updateRequest  *pb.UpdateIssueRequest
		checkIssue     func(*testing.T, *pb.Issue)
		expectedStatus codes.Code
	}{
		"update title": {
			updateRequest: &pb.UpdateIssueRequest{
				Title:      "Updated title",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Updated title", issue.Title)
			},
			expectedStatus: codes.OK,
		},
		"update description": {
			updateRequest: &pb.UpdateIssueRequest{
				Description: "Updated description",
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Updated description", issue.Description)
			},
			expectedStatus: codes.OK,
		},
		"update priority": {
			updateRequest: &pb.UpdateIssueRequest{
				Priority:   grpc_marshalling.IssuePriorityInverse(entities.IssuePriorities.Minor),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"priority"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, grpc_marshalling.IssuePriorityInverse(entities.IssuePriorities.Minor), issue.Priority)
			},
			expectedStatus: codes.OK,
		},
		"update status": {
			updateRequest: &pb.UpdateIssueRequest{
				StatusId:   grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Duplicate.ID),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status_id"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Duplicate.ID), issue.StatusId)
			},
			expectedStatus: codes.OK,
		},
		"update assignee": {
			updateRequest: &pb.UpdateIssueRequest{
				AssigneeId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"assignee_id"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Krosh.ID), issue.GetAssigneeId())
			},
			expectedStatus: codes.OK,
		},
		"clear assignee": {
			updateRequest: &pb.UpdateIssueRequest{
				AssigneeId: "",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"assignee_id"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Nil(t, issue.AssigneeId)
			},
			expectedStatus: codes.OK,
		},
		"update milestone": {
			updateRequest: &pb.UpdateIssueRequest{
				MilestoneId: grpc_marshalling.IDInverse(milestone2.ID),
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"milestone_id"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, grpc_marshalling.IDInverse(milestone2.ID), issue.Milestone.Id)
			},
		},
		"clear milestone": {
			updateRequest: &pb.UpdateIssueRequest{
				MilestoneId: "",
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"milestone_id"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Nil(t, issue.Milestone)
			},
		},
		"update milestone from another repo": {
			updateRequest: &pb.UpdateIssueRequest{
				MilestoneId: grpc_marshalling.IDInverse(milestone3.ID),
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"milestone_id"}},
			},
			expectedStatus: codes.NotFound,
		},
		"update deadline": {
			updateRequest: &pb.UpdateIssueRequest{
				Deadline:   grpc.TimeToProtocTs(deadline),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"deadline"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, grpc.TimeToProtocTs(deadline), issue.GetDeadline())
			},
			expectedStatus: codes.OK,
		},
		"clear deadline": {
			updateRequest: &pb.UpdateIssueRequest{
				Deadline:   nil,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"deadline"}},
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Nil(t, issue.GetDeadline())
			},
			expectedStatus: codes.OK,
		},
		"issue not found": {
			updateRequest: &pb.UpdateIssueRequest{
				Id:    "123456789",
				Title: "Updated title",
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"title"},
				},
			},
			checkIssue:     func(t *testing.T, issue *pb.Issue) {},
			expectedStatus: codes.NotFound,
		},
		"permission denied": {
			user: suite.users.Kopatych,
			updateRequest: &pb.UpdateIssueRequest{
				Title: "Updated title",
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"title"},
				},
			},
			checkIssue:     func(t *testing.T, issue *pb.Issue) {},
			expectedStatus: codes.PermissionDenied,
		},
		"revision check ok": {
			updateRequest: &pb.UpdateIssueRequest{
				Description:  "Updated description",
				UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"description"}},
				BaseRevision: utils.PtrFromValue("1"),
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Updated description", issue.Description)
			},
			expectedStatus: codes.OK,
		},
		"revision conflict": {
			updateRequest: &pb.UpdateIssueRequest{
				Description:  "Updated description",
				UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"description"}},
				BaseRevision: utils.PtrFromValue("2"),
			},
			checkIssue:     func(t *testing.T, issue *pb.Issue) {},
			expectedStatus: codes.FailedPrecondition,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			defaultIssueID := createDefaultIssue()
			if tc.updateRequest.Id == "" {
				tc.updateRequest.Id = grpc_marshalling.IDInverse(defaultIssueID)
			}

			if tc.user == nil {
				tc.user = suite.users.Admin
			}
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)

			resp, err := client.Update(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			updatedIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, updatedIssue)
				require.Equal(t, grpc_marshalling.IDInverse(author.ID), updatedIssue.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), updatedIssue.UpdatedBy)
				require.Greater(t, updatedIssue.UpdatedAt.AsTime(), updatedIssue.CreatedAt.AsTime())
			}
			//yarequire.ProtoDumpFixture(t, updatedIssue)
			yarequire.ProtoCompareWithFixture(t, updatedIssue,
				protocmp.IgnoreFields(
					&pb.Issue{},
					"id",
					"public_id",
					"repo_id",
					"created_at",
					"updated_at",
					"started_at",
					"completed_at",
				),
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)

			fetchedIssue, err := client.Get(ctx, &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: updatedIssue.Id},
			})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, fetchedIssue)
				require.Equal(t, grpc_marshalling.IDInverse(author.ID), fetchedIssue.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), fetchedIssue.UpdatedBy)
				require.Greater(t, fetchedIssue.UpdatedAt.AsTime(), fetchedIssue.CreatedAt.AsTime())
			}
			yarequire.ProtoCompareWithFixture(t, fetchedIssue,
				protocmp.IgnoreFields(
					&pb.Issue{},
					"id",
					"public_id",
					"repo_id",
					"created_at",
					"updated_at",
					"started_at",
					"completed_at",
				),
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateIssueVisibility() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	issue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Initial Issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	testCases := map[string]struct {
		user           *entities.User
		request        *pb.UpdateIssueVisibilityRequest
		expectedStatus codes.Code
	}{
		"author updates visibility": {
			user: suite.users.Krosh,
			request: &pb.UpdateIssueVisibilityRequest{
				Id:         grpc_marshalling.IDInverse(issue.ID),
				Visibility: pb.Issue_VISIBILITY_PUBLIC,
			},
			expectedStatus: codes.OK,
		},
		"random user tries to update visibility": {
			user: suite.users.Slowpoke,
			request: &pb.UpdateIssueVisibilityRequest{
				Id:         grpc_marshalling.IDInverse(issue.ID),
				Visibility: pb.Issue_VISIBILITY_PUBLIC,
			},
			expectedStatus: codes.PermissionDenied,
		},
		"admin updates visibility": {
			user: suite.users.Admin,
			request: &pb.UpdateIssueVisibilityRequest{
				Id:         grpc_marshalling.IDInverse(issue.ID),
				Visibility: pb.Issue_VISIBILITY_PUBLIC,
			},
			expectedStatus: codes.OK,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.UpdateVisibility(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus == codes.OK {
				updatedIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
				require.NoError(t, err)
				require.Equal(t, tc.request.Visibility, updatedIssue.Visibility)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateIssueLabels() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	anotherRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

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
	label3 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: anotherRepoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Grey,
	})

	createDefaultIssue := func() uint64 {
		issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "Default Issue",
			Visibility: entities.IssueVisibilities.Public,
		})
		return issue.ID
	}

	toManyLabels := make([]*entities.Label, services.MaxIssueLabelsCount+1)
	for i := 0; i < len(toManyLabels); i++ {
		toManyLabels[i] = suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: repoID,
			Name:   fmt.Sprintf("Label %d", i),
		})
	}

	tt := map[string]struct {
		initialRequest *pb.UpdateIssueLabelsRequest
		updateRequest  *pb.UpdateIssueLabelsRequest
		user           *entities.User
		checkIssue     func(*testing.T, *pb.Issue)
		expectedStatus codes.Code
	}{
		"add label": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label1.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
		},
		"add labels": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(label2.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label1.ID),
					grpc_marshalling.IDInverse(label2.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
		},
		"remove label": {
			initialRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(label2.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label2.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
		},
		"remove labels": {
			initialRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(label2.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
					{
						Id:     grpc_marshalling.IDInverse(label2.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Empty(t, issue.Labels)
			},
		},
		"add and remove same label": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Empty(t, issue.Labels)
			},
		},
		"add not existing label": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     "123456789",
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
		"remove not existing label": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     "123456789",
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
		"add already existing in issue label": {
			initialRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label1.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
		},
		"remove not existing in issue label": {
			initialRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label2.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label1.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
		},
		"add label from another repo": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label3.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.NotFound,
		},
		"too many labels": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: func() []*pb.IdDelta {
					labelDeltas := make([]*pb.IdDelta, len(toManyLabels))
					for i, label := range toManyLabels {
						labelDeltas[i] = &pb.IdDelta{
							Id:     grpc_marshalling.IDInverse(label.ID),
							Action: pb.DeltaAction_ADD,
						}
					}
					return labelDeltas
				}(),
			},
			user:           suite.users.Admin,
			expectedStatus: codes.FailedPrecondition,
		},
		"permission denied": {
			updateRequest: &pb.UpdateIssueLabelsRequest{
				LabelDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(label1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Slowpoke,
			expectedStatus: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			defaultIssueID := createDefaultIssue()

			if tc.initialRequest != nil {
				tc.initialRequest.Id = grpc_marshalling.IDInverse(defaultIssueID)
				_, err := client.UpdateLabels(ctx, tc.initialRequest)
				yarequire.ProtoStatusEqual(t, codes.OK, err)
			}

			tc.updateRequest.Id = grpc_marshalling.IDInverse(defaultIssueID)
			resp, err := client.UpdateLabels(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			updatedIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, updatedIssue)
			}
			//yarequire.ProtoDumpFixture(t, updatedIssue)
			yarequire.ProtoCompareWithFixture(t, updatedIssue,
				protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)

			fetchedIssue, err := client.Get(ctx, &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: updatedIssue.Id},
			})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, fetchedIssue)
			}
			yarequire.ProtoCompareWithFixture(t, fetchedIssue,
				protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestUpdateIssuesWithNotifications() {
	t := suite.T()
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	commentClient := pb.NewIssueCommentServiceClient(suite.grpcClient)
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repoID := suite.repos.Alpha.ID
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)

	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "First issue",
		Description: "This is my first issue",
		Visibility:  entities.IssueVisibilities.Public,
		NotifyOpts: entities.NotifyOptions{
			SubscribeMe:       true,
			NotifySubscribers: true,
		},
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
	createCommentRequest := &pb.CreateIssueCommentRequest{
		IssueId: grpc_marshalling.IDInverse(issue.ID),
		Body:    "Simple comment",
	}

	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	kopCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	// subscribe krosh and kopatych through comments
	_, err := commentClient.Create(kroshCtx, createCommentRequest)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
	_, err = commentClient.Create(kopCtx, createCommentRequest)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
	suite.ClearNotifyMessages()

	// change priotiry
	_, err = issueClient.Update(adminCtx,
		&pb.UpdateIssueRequest{
			Id:          grpc_marshalling.IDInverse(issue.ID),
			Priority:    grpc_marshalling.IssuePriorityInverse(entities.IssuePriorities.Critical),
			Description: "This is my second issue",
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"priority", "description"},
			},
		},
	)
	require.NoError(t, err)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)

	flatten := func(n []*entities.CloudNotifyRequest) [][]string {
		var simplified [][]string
		for _, msg := range n {
			simplified = append(simplified, []string{msg.Template, msg.Receiver.ID})
		}
		return simplified
	}

	f := flatten(suite.NotifyMessages(context.Background()))
	require.ElementsMatch(t, f, [][]string{
		[]string{"src.issue.update", suite.users.Krosh.Identity.ID},
		[]string{"src.issue.update", suite.users.Kopatych.Identity.ID},
	})
	suite.ClearNotifyMessages()

	updateVisibilityRequest := &pb.UpdateIssueVisibilityRequest{
		Id:         grpc_marshalling.IDInverse(issue.ID),
		Visibility: pb.Issue_VISIBILITY_PRIVATE,
	}

	// admin makes issue private, 1 update for kopatych
	_, err = issueClient.UpdateVisibility(adminCtx, updateVisibilityRequest)
	require.NoError(t, err)

	// message goes only to Kopatych (admin). will not go to admin (as author)
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)

	f = flatten(suite.NotifyMessages(context.Background()))
	require.ElementsMatch(t, f, [][]string{
		[]string{"src.issue.update", suite.users.Kopatych.Identity.ID},
	})

}

func (suite *RwApiTestSuite) TestIssueNotificationsOnAssigneeUpdate() {
	t := suite.T()
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	commentClient := pb.NewIssueCommentServiceClient(suite.grpcClient)
	adminCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repoID := suite.repos.Alpha.ID
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "First issue",
		Visibility: entities.IssueVisibilities.Public,
		NotifyOpts: entities.NotifyOptions{
			SubscribeMe:       true,
			NotifySubscribers: true,
		},
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)

	updateAssigneeRequest := &pb.UpdateIssueRequest{
		Id:         grpc_marshalling.IDInverse(issue.ID),
		AssigneeId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"assignee_id"},
		},
	}
	// admin updates assignee and subscribes krosh
	_, err := issueClient.Update(adminCtx, updateAssigneeRequest)
	require.NoError(t, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)

	createCommentRequest := &pb.CreateIssueCommentRequest{
		IssueId: grpc_marshalling.IDInverse(issue.ID),
		Body:    "Simple comment",
	}
	// admin creates comment and krosh should get a notification
	_, err = commentClient.Create(adminCtx, createCommentRequest)
	require.NoError(t, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)

	notifyHistory := suite.NotifyMessages(context.Background())
	updates := functools.Filter(notifyHistory, func(req *entities.CloudNotifyRequest) bool {
		return req.Template == "src.issue.comment"
	})

	require.Len(t, updates, 1)
	require.Equal(t, suite.users.Krosh.Username, updates[0].Receiver.ID)
}
