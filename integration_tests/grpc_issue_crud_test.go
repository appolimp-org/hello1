package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/adapters/opensearch/mappings"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/revision"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type makeIssueOptions struct {
	ID             uint64
	RepoID         uint64
	Title          string
	Description    string
	Priority       entities.IssuePriority
	Status         entities.IssueStatus
	AssigneeID     *uint64
	MilestoneID    *uint64
	LabelIDs       []uint64
	PullRequestIDs []uint64
	Visibility     entities.IssueVisibility
	Votes          int32
	NotifyOpts     entities.NotifyOptions
	Deadline       *time.Time
}

func (suite *RwApiTestSuite) makeIssue(user *entities.User, opts *makeIssueOptions) *entities.Issue {
	t := suite.T()

	issue := &entities.Issue{
		ID:          opts.ID,
		RepoID:      opts.RepoID,
		Title:       opts.Title,
		Description: opts.Description,
		Priority:    opts.Priority,
		StatusID:    opts.Status.ID,
		AssigneeID:  opts.AssigneeID,
		MilestoneID: opts.MilestoneID,
		AuthorID:    user.ID,
		UpdatedBy:   user.ID,
		Visibility:  opts.Visibility,
		Votes:       opts.Votes,
		Deadline:    opts.Deadline,
	}

	if issue.Priority == 0 {
		issue.Priority = entities.IssuePriorities.Normal
	}

	if issue.StatusID == 0 {
		issue.StatusID = entities.IssueStatuses.Open.ID
	}

	if issue.Visibility == "" {
		issue.Visibility = entities.IssueVisibilities.Public
	}

	if issue.Title == "" {
		issue.Title = "Title"
	}

	issueID, err := suite.IssueService.Create(context.Background(), issue, opts.LabelIDs, []uint64{}, opts.PullRequestIDs, user, opts.NotifyOpts)
	require.NoError(t, err)

	issue, err = suite.IssueRepo.Get(context.Background(), issueID)
	require.NoError(t, err)

	return issue
}

func (suite *RwApiTestSuite) TestGrpcCreateIssue() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	anotherRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	milestone1 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:   repoID,
		Name:     "Milestone",
		Deadline: utils.PtrFromValue(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	milestone2 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID:   anotherRepoID,
		Name:     "Milestone",
		Deadline: utils.PtrFromValue(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
	})

	label1 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Task",
		Color:  entities.PresetLabelColors.Grey,
	})
	label2 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Red,
	})
	label3 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: anotherRepoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Red,
	})

	deadline := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tt := map[string]struct {
		request        *pb.CreateIssueRequest
		user           *entities.User
		checkIssue     func(*testing.T, *pb.Issue)
		expectedStatus codes.Code
	}{
		"simple issue": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Title:  "Simple issue",
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Simple issue", issue.Title)
				require.Nil(t, issue.AssigneeId)
				require.Equal(t, pb.Issue_VISIBILITY_PUBLIC, issue.Visibility) // default visibility
			},
			expectedStatus: codes.OK,
		},
		"wrong repo issue": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId: "123456789",
				Title:  "Wrong repo issue",
			},
			expectedStatus: codes.NotFound,
		},
		"private issue with description": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Secret issue",
				Description: "Very secret issue",
				Visibility:  pb.Issue_VISIBILITY_PRIVATE,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Secret issue", issue.Title)
				require.Equal(t, "Very secret issue", issue.Description)
				require.Equal(t, pb.Issue_VISIBILITY_PRIVATE, issue.Visibility)
			},
			expectedStatus: codes.OK,
		},
		"create issue with priority": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with priority",
				Priority:    utils.PtrFromValue(pb.Issue_PRIORITY_CRITICAL),
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Issue", issue.Title)
				require.Equal(t, "Issue with priority", issue.Description)
				require.Equal(t, pb.Issue_PRIORITY_CRITICAL, issue.Priority)
			},
			expectedStatus: codes.OK,
		},
		"create issue with status": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with status",
				StatusId:    utils.PtrFromValue(grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID)),
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Issue", issue.Title)
				require.Equal(t, "Issue with status", issue.Description)
				require.Equal(t, grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID), issue.StatusId)
				require.Equal(t, pb.Issue_VISIBILITY_PUBLIC, issue.Visibility)
				require.NotNil(t, issue.GetStartedAt())
				require.Nil(t, issue.GetCompletedAt())
			},
			expectedStatus: codes.OK,
		},
		"create issue with status closed": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with status",
				StatusId:    utils.PtrFromValue(grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Closed.ID)),
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Nil(t, issue.GetStartedAt())
				require.NotNil(t, issue.GetCompletedAt())
			},
			expectedStatus: codes.OK,
		},
		"issue with assignee": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with assignee",
				AssigneeId:  utils.PtrFromValue(grpc_marshalling.IDInverse(suite.users.Krosh.ID)),
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Issue", issue.Title)
				require.Equal(t, "Issue with assignee", issue.Description)
				require.Equal(t, grpc_marshalling.IDInverse(suite.users.Krosh.ID), *issue.AssigneeId)
				require.Equal(t, pb.Issue_VISIBILITY_PUBLIC, issue.Visibility)
			},
			expectedStatus: codes.OK,
		},
		"issue with milestone": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with milestone",
				MilestoneId: utils.PtrFromValue(grpc_marshalling.IDInverse(milestone1.ID)),
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, "Issue", issue.Title)
				require.Equal(t, "Issue with milestone", issue.Description)
				require.Equal(t, grpc_marshalling.IDInverse(milestone1.ID), issue.Milestone.Id)
				require.Equal(t, pb.Issue_VISIBILITY_PUBLIC, issue.Visibility)
			},
			expectedStatus: codes.OK,
		},
		"issue with another repo milestone": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with milestone",
				MilestoneId: utils.PtrFromValue(grpc_marshalling.IDInverse(milestone2.ID)),
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
			},
			expectedStatus: codes.NotFound,
		},
		"create issue with labels": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Title:  "Issue with labels",
				LabelIds: []string{
					grpc_marshalling.IDInverse(label1.ID),
					grpc_marshalling.IDInverse(label2.ID),
				},
				Visibility: pb.Issue_VISIBILITY_PUBLIC,
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(label1.ID),
					grpc_marshalling.IDInverse(label2.ID),
				}, functools.Map(issue.Labels, (*pb.Label).GetId))
			},
			expectedStatus: codes.OK,
		},
		"create issue with another repo label": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Title:  "Issue with labels",
				LabelIds: []string{
					grpc_marshalling.IDInverse(label1.ID),
					grpc_marshalling.IDInverse(label3.ID), // label3 from another repo
				},
				Visibility: pb.Issue_VISIBILITY_PUBLIC,
			},
			expectedStatus: codes.NotFound,
		},
		"create issue with deadline": {
			user: suite.users.Admin,
			request: &pb.CreateIssueRequest{
				RepoId:      grpc_marshalling.IDInverse(repoID),
				Title:       "Issue",
				Description: "Issue with status",
				Visibility:  pb.Issue_VISIBILITY_PUBLIC,
				Deadline:    grpc.TimeToProtocTs(deadline),
			},
			checkIssue: func(t *testing.T, issue *pb.Issue) {
				require.NotNil(t, issue.GetDeadline())
				require.WithinDuration(t, deadline, *grpc.ProtocTsToTimeNullable(issue.GetDeadline()), time.Second)
			},
			expectedStatus: codes.OK,
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			if tc.request.Visibility == pb.Issue_VISIBILITY_UNSPECIFIED {
				tc.request.Visibility = pb.Issue_VISIBILITY_PUBLIC
			}
			resp, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			createdIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, createdIssue)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdIssue.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdIssue.UpdatedBy)
			}

			//yarequire.ProtoDumpFixture(t, createdIssue)
			yarequire.ProtoCompareWithFixture(t, createdIssue,
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
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Revision{}, "count"),
			)

			fetchedIssue, err := client.Get(ctx, &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: createdIssue.Id},
			})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, fetchedIssue)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdIssue.AuthorId)
				require.Equal(t, grpc_marshalling.IDInverse(tc.user.ID), createdIssue.UpdatedBy)
			}
			yarequire.ProtoCompareWithFixture(t, createdIssue,
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
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Revision{}, "count"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestIssueUsersRelevantRepos() {
	t := suite.T()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	client := pb.NewIssueServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	op, err := client.Create(ctx, &pb.CreateIssueRequest{
		RepoId:     grpc_marshalling.IDInverse(repoID),
		Title:      "test",
		Visibility: pb.Issue_VISIBILITY_PUBLIC,
		AssigneeId: utils.PtrFromValue(grpc_marshalling.IDInverse(suite.users.Krosh.ID)),
	})
	require.NoError(t, err)

	issue := testutils.UnmarshalGrpcResult[*pb.Issue](t, op)

	_, err = client.Update(ctx, &pb.UpdateIssueRequest{
		Id:         issue.Id,
		AssigneeId: grpc_marshalling.IDInverse(suite.users.Barash.ID),
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"assignee_id"},
		},
	})
	require.NoError(t, err)

	kopatychRelevantRepos, err := suite.UserRelevantReposRepository.GetRelevantRepos(ctx, suite.users.Kopatych.ID)
	require.NoError(t, err)

	kroshRelevantRepos, err := suite.UserRelevantReposRepository.GetRelevantRepos(ctx, suite.users.Krosh.ID)
	require.NoError(t, err)

	barashRelevantRepos, err := suite.UserRelevantReposRepository.GetRelevantRepos(ctx, suite.users.Barash.ID)
	require.NoError(t, err)

	require.Contains(t, kopatychRelevantRepos, repoID)
	require.Contains(t, kroshRelevantRepos, repoID)
	require.Contains(t, barashRelevantRepos, repoID)
}

func (suite *RwApiTestSuite) TestParallelCreateIssue() {
	t := suite.T()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	client := pb.NewIssueServiceClient(suite.grpcClient)
	ctx1 := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	ctx2 := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	var issueID uint64
	err := suite.TxManager.WithTx(ctx1, func(ctx context.Context) error {
		publicID, err := suite.PublicIDService.GetNextID(ctx, repoID, entities.PublicCounterTypes.Issue)
		require.NoError(t, err)

		// can not create issue in another context (locked)
		ctx2WithTimeout, cancel := context.WithTimeout(ctx2, 200*time.Millisecond)
		defer cancel()
		_, err = suite.IssueService.Create(ctx2WithTimeout, &entities.Issue{
			RepoID:     repoID,
			Title:      "Another issue",
			AuthorID:   suite.users.Admin.ID,
			UpdatedBy:  suite.users.Admin.ID,
			Visibility: entities.IssueVisibilities.Public,
		}, nil, []uint64{}, []uint64{}, suite.users.Admin, entities.NotifyOptions{})
		require.ErrorIs(t, err, context.DeadlineExceeded)

		// can create issue inside context
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		issueID, err = suite.IssueRepo.Create(ctxWithTimeout, &entities.Issue{
			PublicID:   publicID,
			RepoID:     repoID,
			Title:      "Issue",
			AuthorID:   suite.users.Admin.ID,
			UpdatedBy:  suite.users.Admin.ID,
			Visibility: entities.IssueVisibilities.Public,
		})
		require.NoError(t, err)
		return err
	})
	require.NoError(t, err)

	res, err := client.Get(ctx2, &pb.GetIssueRequest{
		Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(issueID)},
	})
	require.NoError(t, err)
	require.Equal(t, grpc_marshalling.IDInverse(issueID), res.Id)
	require.Equal(t, "1", res.PublicId)
	require.Equal(t, "Issue", res.Title)

	// can create in another context (unlocked)
	anotherIssueID, err := suite.IssueService.Create(ctx2, &entities.Issue{
		RepoID:     repoID,
		Title:      "Another issue",
		AuthorID:   suite.users.Admin.ID,
		UpdatedBy:  suite.users.Admin.ID,
		Visibility: entities.IssueVisibilities.Public,
	}, nil, []uint64{}, []uint64{}, suite.users.Admin, entities.NotifyOptions{})
	require.NoError(t, err)

	res, err = client.Get(ctx2, &pb.GetIssueRequest{
		Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(anotherIssueID)},
	})
	require.NoError(t, err)
	require.Equal(t, grpc_marshalling.IDInverse(anotherIssueID), res.Id)
	require.Equal(t, "2", res.PublicId)
	require.Equal(t, "Another issue", res.Title)
}

func (suite *RwApiTestSuite) TestGrpcGetIssue() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID

	label := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Existing Label",
		Color:  "FF0000",
	})
	deletedLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Deleted Label",
		Color:  "FF0000",
	})

	deletedMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Deleted milestone",
	})

	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "Kopatych issue",
		Visibility:  entities.IssueVisibilities.Public,
		LabelIDs:    []uint64{label.ID, deletedLabel.ID},
		MilestoneID: &deletedMilestone.ID,
	})

	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		RepoID:  &repoID,
		IssueID: &issue.ID,
	})

	require.NoError(t, suite.LabelService.Delete(context.Background(), deletedLabel, suite.users.Admin))
	require.NoError(t, suite.MilestoneService.Delete(context.Background(), deletedMilestone, suite.users.Admin))

	tt := map[string]struct {
		user           *entities.User
		request        *pb.GetIssueRequest
		expectedTitle  string
		expectedStatus codes.Code
	}{
		"get by ID": {
			user: suite.users.Kopatych,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(issue.ID)},
			},
			expectedTitle:  "Kopatych issue",
			expectedStatus: codes.OK,
		},
		"not found by ID": {
			user: suite.users.Kopatych,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: "123456789"},
			},
			expectedStatus: codes.NotFound,
		},
		"get by PublicID": {
			user: suite.users.Krosh,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_FullPublicId{
					FullPublicId: &pb.IssueIdentity{
						RepoId:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						IssuePublicId: grpc_marshalling.IDInverse(issue.PublicID),
					},
				},
			},
			expectedStatus: codes.OK,
		},
		"not found by PublicID": {
			user: suite.users.Krosh,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_FullPublicId{
					FullPublicId: &pb.IssueIdentity{
						RepoId:        grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						IssuePublicId: grpc_marshalling.IDInverse(123456789),
					},
				},
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			res, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			require.NoError(t, err)
			require.Equal(t, grpc_marshalling.IDInverse(issue.ID), res.Id)

			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetPrivateIssue() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	privateIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Private Issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	testCases := map[string]struct {
		user           *entities.User
		request        *pb.GetIssueRequest
		expectedStatus codes.Code
	}{
		"author get private issue": {
			user: suite.users.Krosh,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(privateIssue.ID)},
			},
			expectedStatus: codes.OK,
		},
		"random user get private issue": {
			user: suite.users.Slowpoke,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(privateIssue.ID)},
			},
			expectedStatus: codes.PermissionDenied,
		},
		"admin get private issue": {
			user: suite.users.Admin,
			request: &pb.GetIssueRequest{
				Issue: &pb.GetIssueRequest_Id{Id: grpc_marshalling.IDInverse(privateIssue.ID)},
			},
			expectedStatus: codes.OK,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			res, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			//yarequire.ProtoDumpFixture(t, res)
			yarequire.ProtoCompareWithFixture(t, res,
				protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
			)

		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGetBulkIssues() {
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	repo2ID := suite.repos.AuthRepoPrivate.ID

	suite.addRole(suite.T(), suite.users.Krosh, suite.repos.AuthRepoPrivate, iam.Roles.RepositoriesMaintainer)

	// Create issues in the repository
	issues := []*entities.Issue{
		suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "First issue",
			Visibility: entities.IssueVisibilities.Public,
		}),
		suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "Second issue",
			Visibility: entities.IssueVisibilities.Private,
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "Third issue",
			Visibility: entities.IssueVisibilities.Private,
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:     repo2ID,
			Title:      "Forth issue",
			Visibility: entities.IssueVisibilities.Public,
		}),
	}
	issuesPB := functools.Map(issues, grpc_marshalling.EntityToPB.Issue)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.GetBulkIssuesRequest
		expectedIssues []*pb.Issue
		expectedStatus codes.Code
	}{
		"get multiple issues": {
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{
					grpc_marshalling.IDInverse(issues[0].ID),
					grpc_marshalling.IDInverse(issues[1].ID),
				},
			},
			expectedIssues: []*pb.Issue{
				issuesPB[0],
				issuesPB[1],
			},
			expectedStatus: codes.OK,
		},
		"get single issue": {
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{grpc_marshalling.IDInverse(issues[2].ID)},
			},
			expectedIssues: []*pb.Issue{
				issuesPB[2],
			},
			expectedStatus: codes.OK,
		},
		"get not existing issue": {
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{"123456789"},
			},
			expectedIssues: nil,
			expectedStatus: codes.OK,
		},
		"empty issue IDs": {
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{},
			},
			expectedIssues: nil,
			expectedStatus: codes.InvalidArgument,
		},
		"get issues by kopyatych": {
			user: suite.users.Kopatych,
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{
					grpc_marshalling.IDInverse(issues[0].ID),
					grpc_marshalling.IDInverse(issues[1].ID),
					grpc_marshalling.IDInverse(issues[2].ID),
					grpc_marshalling.IDInverse(issues[3].ID),
				},
			},
			expectedIssues: []*pb.Issue{
				issuesPB[0],
				issuesPB[1],
			},
			expectedStatus: codes.OK,
		},
		"get issues by krosh": {
			user: suite.users.Krosh,
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{
					grpc_marshalling.IDInverse(issues[0].ID),
					grpc_marshalling.IDInverse(issues[1].ID),
					grpc_marshalling.IDInverse(issues[2].ID),
					grpc_marshalling.IDInverse(issues[3].ID),
				},
			},
			expectedIssues: []*pb.Issue{
				issuesPB[0],
				issuesPB[2],
				issuesPB[3],
			},
			expectedStatus: codes.OK,
		},
		"get issues by admin": {
			user: suite.users.Admin,
			request: &pb.GetBulkIssuesRequest{
				IssueIds: []string{
					grpc_marshalling.IDInverse(issues[0].ID),
					grpc_marshalling.IDInverse(issues[1].ID),
					grpc_marshalling.IDInverse(issues[2].ID),
					grpc_marshalling.IDInverse(issues[3].ID),
				},
			},
			expectedIssues: []*pb.Issue{
				issuesPB[0],
				issuesPB[1],
				issuesPB[2],
				issuesPB[3],
			},
			expectedStatus: codes.OK,
		},
	}

	for tn, tc := range tt {
		suite.T().Run(tn, func(t *testing.T) {
			if tc.user == nil {
				tc.user = suite.users.Admin
			}
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.GetBulk(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			if tc.expectedStatus != codes.OK {
				return
			}

			require.NoError(t, err)
			require.Len(t, resp.Issues, len(tc.expectedIssues))

			for i := range resp.Issues {
				yarequire.ProtoEqual(t, tc.expectedIssues[i], resp.Issues[i])
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListIssues() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	suite.RestoreOpensearch()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	var pageToken string

	label := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Existing Label",
		Color:  "FF0000",
	})
	deletedLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Deleted Label",
		Color:  "FF0000",
	})

	milestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone",
	})
	deletedMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Deleted milestone",
	})

	// test issues
	issues := []*entities.Issue{
		suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "First issue",
			Visibility: entities.IssueVisibilities.Public,
			LabelIDs:   []uint64{label.ID, deletedLabel.ID},
		}),
		suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Second issue",
			Priority:    entities.IssuePriorities.Critical,
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &deletedMilestone.ID,
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Third issue",
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &milestone.ID,
			LabelIDs:    []uint64{deletedLabel.ID},
		}),
	}
	issuesPB := functools.Map(issues, grpc_marshalling.EntityToPB.Issue)

	// create comment for second issue
	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		RepoID:  &repoID,
		IssueID: &issues[1].ID,
	})
	issuesPB[1].CommentsCount = 1

	require.NoError(t, suite.LabelService.Delete(context.Background(), deletedLabel, suite.users.Admin))
	require.NoError(t, suite.MilestoneService.Delete(context.Background(), deletedMilestone, suite.users.Admin))

	deletedIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Deleted issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	err := suite.IssueService.Delete(context.Background(), deletedIssue, suite.users.Admin.ID, entities.NotifyOptions{})
	require.NoError(t, err)

	// issue from another repository
	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Alpha issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	tests := []struct {
		name           string
		user           *entities.User
		request        *pb.ListIssuesRequest
		useIndex       bool
		PrevPageToken  string
		NextPageToken  string
		expectedStatus codes.Code
		savePageToken  bool
	}{
		{
			name: "ok first page",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(1)),
			},
			NextPageToken: testutils.Presence,
			savePageToken: true,
		},
		{
			name: "ok second page",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:    grpc_marshalling.IDInverse(repoID),
				PageSize:  utils.PtrFromValue(uint64(1)),
				PageToken: &pageToken,
			},
			PrevPageToken: testutils.Presence,
			NextPageToken: testutils.Presence,
		},
		{
			name: "ok all issues",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			},
		},
		{
			name: "sorted by public id",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(2)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "public_id",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
			NextPageToken: testutils.Presence,
		},
		{
			name: "sorted by creation date",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(2)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "created_at",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
			},
			NextPageToken: testutils.Presence,
		},
		{
			name: "sorted by priority ascending",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "priority",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
		},
		{
			name: "sorted by priority descending",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "priority",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
			},
		},
		{
			name: "list issues by kopatych",
			user: suite.users.Kopatych,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			},
			expectedStatus: codes.OK,
		},
		{
			name: "list issues by krosh",
			user: suite.users.Krosh,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
			},
			expectedStatus: codes.OK,
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {

			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.List(ctx, tc.request)

			if tc.expectedStatus != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.expectedStatus, st.Code())
				return
			}
			require.NoError(t, err)

			if tc.NextPageToken == testutils.Presence {
				require.NotEmpty(t, resp.NextPageToken)
				tc.NextPageToken = resp.NextPageToken
			}
			if tc.PrevPageToken == testutils.Presence {
				require.NotEmpty(t, resp.PrevPageToken)
				tc.PrevPageToken = resp.PrevPageToken
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.ListIssuesResponse{}, "prev_page_token", "next_page_token"),
				protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
			)

			if tc.savePageToken {
				pageToken = resp.NextPageToken
			}

		})
	}

	suite.T().Run("pagination", func(t *testing.T) {

		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		resp, err := client.List(ctx, &pb.ListIssuesRequest{
			RepoId:   grpc_marshalling.IDInverse(repoID),
			PageSize: utils.PtrFromValue(uint64(1)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)
		require.Equal(t, issuesPB[0].Id, resp.Issues[0].Id)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")

		resp, err = client.List(ctx, &pb.ListIssuesRequest{
			RepoId:    grpc_marshalling.IDInverse(repoID),
			PageToken: &resp.NextPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 2)
		require.Equal(t, issuesPB[1].Id, resp.Issues[0].Id)
		require.Equal(t, issuesPB[2].Id, resp.Issues[1].Id)

		require.True(t, resp.NextPageToken == "")
		require.True(t, resp.PrevPageToken != "")
		resp, err = client.List(ctx, &pb.ListIssuesRequest{
			RepoId:    grpc_marshalling.IDInverse(repoID),
			PageToken: &resp.PrevPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)
		require.Equal(t, issuesPB[0].Id, resp.Issues[0].Id)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")

	})
}

func (suite *RwApiTestSuite) TestGrpcListIssuesWithQuery() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	suite.RestoreOpensearch()
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	var pageToken string

	label := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Existing Label",
		Color:  "FF0000",
	})
	deletedLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Deleted Label",
		Color:  "FF0000",
	})

	milestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone",
	})
	deletedMilestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Deleted milestone",
	})

	// test issues
	issues := []*entities.Issue{
		suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "First issue",
			Description: "First issue description",
			Visibility:  entities.IssueVisibilities.Public,
			LabelIDs:    []uint64{label.ID, deletedLabel.ID},
			Votes:       1,
		}),
		suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Issue second",
			Description: "Second issue description",
			Priority:    entities.IssuePriorities.Critical,
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &deletedMilestone.ID,
			Votes:       3,
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Issue third",
			Description: "Third issue description",
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &milestone.ID,
			LabelIDs:    []uint64{deletedLabel.ID},
			Votes:       0,
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Title",
			Description: "Description",
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &milestone.ID,
			LabelIDs:    []uint64{deletedLabel.ID},
			Votes:       0,
		}),
	}
	issuesPB := functools.Map(issues, grpc_marshalling.EntityToPB.Issue)

	// create comment for second issue
	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		RepoID:  &repoID,
		IssueID: &issues[1].ID,
	})
	issuesPB[1].CommentsCount = 1

	require.NoError(t, suite.LabelService.Delete(context.Background(), deletedLabel, suite.users.Admin))
	require.NoError(t, suite.MilestoneService.Delete(context.Background(), deletedMilestone, suite.users.Admin))

	deletedIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Deleted issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	err := suite.IssueService.Delete(context.Background(), deletedIssue, suite.users.Admin.ID, entities.NotifyOptions{})
	require.NoError(t, err)

	// issue from another repository
	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Alpha issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err = suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name           string
		user           *entities.User
		request        *pb.ListIssuesRequest
		useIndex       bool
		PrevPageToken  string
		NextPageToken  string
		expectedStatus codes.Code
		savePageToken  bool
	}{
		{
			name: "ok first page",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(1)),
				Query:    utils.PtrFromValue("issue"),
			},
			NextPageToken: testutils.Presence,
			savePageToken: true,
		},
		{
			name: "ok second page",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:    grpc_marshalling.IDInverse(repoID),
				PageSize:  utils.PtrFromValue(uint64(1)),
				PageToken: &pageToken,
				Query:     utils.PtrFromValue("issue"),
			},
			PrevPageToken: testutils.Presence,
		},
		{
			name: "ok all issues",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Query:  utils.PtrFromValue("issue"),
			},
		},
		{
			name: "sorted by public id",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "public_id",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
				Query: utils.PtrFromValue("issue"),
			},
		},
		{
			name: "sorted by votes",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "votes",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
				Query: utils.PtrFromValue("issue"),
			},
		},
		{
			name: "sorted by creation date",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "created_at",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
				Query: utils.PtrFromValue("issue"),
			},
		},
		{
			name: "sorted by priority ascending",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "priority",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
				Query: utils.PtrFromValue("issue"),
			},
		},
		{
			name: "sorted by priority descending",
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				PageSize: utils.PtrFromValue(uint64(3)),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "priority",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
				Query: utils.PtrFromValue("issue"),
			},
		},
		{
			name: "list issues by kopatych",
			user: suite.users.Kopatych,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Query:  utils.PtrFromValue("issue"),
			},
			expectedStatus: codes.OK,
		},
		{
			name: "list issues by krosh",
			user: suite.users.Krosh,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Query:  utils.PtrFromValue("issue"),
			},
			expectedStatus: codes.OK,
		},
	}

	for _, useIndex := range []bool{true, false} {
		testSuffix := ""
		if useIndex {
			testSuffix = " with index enabled"
		}
		for _, tc := range tests {
			suite.T().Run(tc.name+testSuffix, func(t *testing.T) {
				if useIndex {
					suite.OpensearchBackendProxy.MakeAvaliable()
				}

				ctx := testutils.AuthorizeGRPC(tc.user.Identity)
				resp, err := client.List(ctx, tc.request)

				if tc.expectedStatus != 0 {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, tc.expectedStatus, st.Code())
					return
				}
				require.NoError(t, err)

				if tc.NextPageToken == testutils.Presence {
					require.NotEmpty(t, resp.NextPageToken)
					tc.NextPageToken = resp.NextPageToken
				}
				if tc.PrevPageToken == testutils.Presence {
					require.NotEmpty(t, resp.PrevPageToken)
					tc.PrevPageToken = resp.PrevPageToken
				}

				// yarequire.ProtoDumpFixture(t, resp)
				yarequire.ProtoCompareWithFixture(t, resp,
					protocmp.IgnoreFields(&pb.ListIssuesResponse{}, "prev_page_token", "next_page_token"),
					protocmp.IgnoreFields(&pb.Issue{}, "id", "public_id", "repo_id", "created_at", "updated_at"),
					protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
					protocmp.IgnoreFields(&pb.Milestone{}, "id", "created_at", "updated_at"),
				)

				if tc.savePageToken {
					pageToken = resp.NextPageToken
				}

				if useIndex {
					suite.OpensearchBackendProxy.MakeUnavaliable()
				}
			})
		}
		if useIndex {
			suite.OpensearchBackendProxy.MakeUnavaliable()
		}
	}

	suite.T().Run("pagination", func(t *testing.T) {
		suite.OpensearchBackendProxy.MakeAvaliable()
		err := suite.OpensearchKit.RefreshIndex(context.Background())
		require.NoError(t, err)

		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

		resp, err := client.List(ctx, &pb.ListIssuesRequest{
			RepoId:   grpc_marshalling.IDInverse(repoID),
			PageSize: utils.PtrFromValue(uint64(1)),
			Query:    utils.PtrFromValue("issue"),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)
		require.Equal(t, issuesPB[0].Id, resp.Issues[0].Id)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")

		resp, err = client.List(ctx, &pb.ListIssuesRequest{
			RepoId:    grpc_marshalling.IDInverse(repoID),
			PageToken: &resp.NextPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
			Query:     utils.PtrFromValue("issue"),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 2)
		require.Equal(t, issuesPB[1].Id, resp.Issues[0].Id)
		require.Equal(t, issuesPB[2].Id, resp.Issues[1].Id)

		require.True(t, resp.NextPageToken == "")
		require.True(t, resp.PrevPageToken != "")
		resp, err = client.List(ctx, &pb.ListIssuesRequest{
			RepoId:    grpc_marshalling.IDInverse(repoID),
			PageToken: &resp.PrevPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
			Query:     utils.PtrFromValue("issue"),
		})
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)
		require.Equal(t, issuesPB[0].Id, resp.Issues[0].Id)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")

	})
}

func (suite *RwApiTestSuite) TestGrpcListUserIssues() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	_ = suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue1",
		Status:     entities.IssueStatuses.Open,
		AssigneeID: utils.PtrFromValue(suite.users.Krosh.ID),
	})

	_ = suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue2",
		Status:     entities.IssueStatuses.Closed,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	_ = suite.makeIssue(suite.users.Pikachu, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue3",
		Status:     entities.IssueStatuses.Declined,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	tests := map[string]struct {
		user             *entities.User
		request          *pb.ListUserIssuesRequest
		expectedLen      int
		expectedRevCount int
		expect           codes.Code
	}{
		// Matches: 1, 2, 3
		"list user issues": {
			user:             suite.users.Admin,
			request:          &pb.ListUserIssuesRequest{},
			expectedLen:      3,
			expectedRevCount: 3,
			expect:           codes.OK,
		},
		// Matches: 1, 2
		"list user issues limited": {
			user: suite.users.Admin,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(2)),
			},
			expectedLen:      2,
			expectedRevCount: 3,
			expect:           codes.OK,
		},
		"list empty": {
			user: suite.users.Slowpoke,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(10)),
			},
			expectedLen:      0,
			expectedRevCount: 0,
			expect:           codes.OK,
		},
		// Matches: 1
		"list filtered": {
			user: suite.users.Admin,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(10)),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "status_id",
											Operator: pagination_pb.Operator_OPERATOR_NE,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Closed.ID),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "status_id",
											Operator: pagination_pb.Operator_OPERATOR_NE,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Declined.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLen:      1,
			expectedRevCount: 3,
			expect:           codes.OK,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListUserIssues(ctx, tc.request)
			require.NoError(t, err)
			yarequire.ProtoStatusEqual(t, tc.expect, err)

			if tc.expect == codes.OK {
				require.Len(t, resp.Issues, tc.expectedLen)
				require.Equal(t, resp.Revision.Count, utils.PtrFromValue(int32(tc.expectedRevCount)))

				// rev sync
				count, err := suite.RevSyncer.ComputeCount(ctx, revision.User(tc.user.ID).Issues)
				require.NoError(t, err)
				require.Equal(t, tc.expectedRevCount, count)
			}

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.ListUserIssuesResponse{}, "next_page_token"),
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
				protocmp.IgnoreFields(&pb.Label{}, "id", "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListUserIssuesWithQuery() {
	t := suite.T()
	suite.RestoreOpensearch()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	_ = suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue1",
		Status:     entities.IssueStatuses.Open,
		AssigneeID: utils.PtrFromValue(suite.users.Krosh.ID),
	})

	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue2 Title",
		Status:     entities.IssueStatuses.Open,
		AssigneeID: utils.PtrFromValue(suite.users.Krosh.ID),
	})

	_ = suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue3",
		Status:     entities.IssueStatuses.Closed,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	issue4 := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue4 Title",
		Status:     entities.IssueStatuses.Closed,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	_ = suite.makeIssue(suite.users.Pikachu, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue5",
		Status:     entities.IssueStatuses.Declined,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	issue6 := suite.makeIssue(suite.users.Pikachu, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Issue6 Title",
		Status:     entities.IssueStatuses.Declined,
		AssigneeID: utils.PtrFromValue(suite.users.Admin.ID),
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)

	tests := map[string]struct {
		user             *entities.User
		request          *pb.ListUserIssuesRequest
		expectedResult   []uint64
		expectedLen      int
		expectedRevCount int
		expect           codes.Code
	}{
		// Matches: 1, 2, 3
		"list user issues": {
			user:             suite.users.Admin,
			request:          &pb.ListUserIssuesRequest{Query: utils.PtrFromValue("title")},
			expectedResult:   []uint64{issue2.ID, issue4.ID, issue6.ID},
			expectedLen:      3,
			expectedRevCount: 6,
			expect:           codes.OK,
		},
		// Matches: 1, 2
		"list user issues limited": {
			user: suite.users.Admin,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(2)),
				Query:    utils.PtrFromValue("title"),
			},
			expectedResult:   []uint64{issue2.ID, issue4.ID},
			expectedLen:      2,
			expectedRevCount: 6,
			expect:           codes.OK,
		},
		"list empty": {
			user: suite.users.Slowpoke,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(10)),
				Query:    utils.PtrFromValue("title"),
			},
			expectedResult:   []uint64{},
			expectedLen:      0,
			expectedRevCount: 0,
			expect:           codes.OK,
		},
		// Matches: 1
		"list filtered": {
			user: suite.users.Admin,
			request: &pb.ListUserIssuesRequest{
				PageSize: utils.PtrFromValue(uint64(10)),
				Query:    utils.PtrFromValue("title"),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "status_id",
											Operator: pagination_pb.Operator_OPERATOR_NE,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Closed.ID),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "status_id",
											Operator: pagination_pb.Operator_OPERATOR_NE,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Declined.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedResult:   []uint64{issue2.ID},
			expectedLen:      1,
			expectedRevCount: 6,
			expect:           codes.OK,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListUserIssues(ctx, tc.request)
			require.NoError(t, err)
			yarequire.ProtoStatusEqual(t, tc.expect, err)

			if tc.expect == codes.OK {
				require.Len(t, resp.Issues, tc.expectedLen)
				require.NotNil(t, resp.Revision.Count)
				require.Equal(t, int32(tc.expectedRevCount), *resp.Revision.Count)
				require.Equal(t, tc.expectedResult, functools.Map(resp.Issues, func(issue *pb.Issue) uint64 {
					id, err := grpc_marshalling.IDDirect(issue.Id)
					require.NoError(t, err)
					return id
				}))

				// rev sync
				count, err := suite.RevSyncer.ComputeCount(ctx, revision.User(tc.user.ID).Issues)
				require.NoError(t, err)
				require.Equal(t, tc.expectedRevCount, count)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcDeleteIssue() {
	t := suite.T()
	client := pb.NewIssueServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	suite.RestoreOpensearch()

	t.Run("delete", func(t *testing.T) {
		issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     suite.repos.Alpha.ID,
			Title:      "Issue can be deleted",
			Visibility: entities.IssueVisibilities.Public,
		})
		issueID := grpc_marshalling.IDInverse(issue.ID)

		_, err := client.Get(ctx, &pb.GetIssueRequest{
			Issue: &pb.GetIssueRequest_Id{Id: issueID},
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		suite.OpensearchKit.RefreshIndex(ctx)
		hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
		require.NoError(t, err)
		require.Len(t, hits, 1)

		res, err := client.Delete(ctx, &pb.DeleteIssueRequest{Id: issueID})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		metadata, err := grpc_marshalling.OperationMetadata(res, &pb.DeleteIssueMetadata{})
		require.NoError(t, err)
		require.Equal(t, issueID, metadata.Id)

		_, err = client.Get(ctx, &pb.GetIssueRequest{
			Issue: &pb.GetIssueRequest_Id{Id: issueID},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		suite.OpensearchKit.RefreshIndex(ctx)

		hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
		require.NoError(t, err)
		require.Len(t, hits, 0)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := client.Delete(ctx, &pb.DeleteIssueRequest{Id: "123456"})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("permission denied", func(t *testing.T) {
		issue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     suite.repos.Alpha.ID,
			Title:      "Issue can be deleted",
			Visibility: entities.IssueVisibilities.Public,
		})
		issueID := grpc_marshalling.IDInverse(issue.ID)

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		_, err := client.Delete(ctx, &pb.DeleteIssueRequest{Id: issueID})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
}
