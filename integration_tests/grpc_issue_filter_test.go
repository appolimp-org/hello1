package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

type issueFilterTest struct {
	orgSlug                                string
	repoID                                 uint64
	repoSlug                               string
	milestone1, milestone2                 *entities.Milestone
	label1, label2                         *entities.Label
	issue1, issue2, issue3, issue4, issue5 *entities.Issue
}

func prepareIssuesForFiltering(suite *RwApiTestSuite) *issueFilterTest {
	t := suite.T()
	suite.RestoreOpensearch()
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	milestone1 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone1",
		Slug:   utils.PtrFromValue("milestone1"),
	})
	milestone2 := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone2",
		Slug:   utils.PtrFromValue("milestone2"),
	})

	label1 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Task",
		Slug:   utils.PtrFromValue("task"),
		Color:  entities.PresetLabelColors.Grey,
	})
	label2 := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Slug:   utils.PtrFromValue("bug"),
		Color:  entities.PresetLabelColors.Red,
	})

	deadline := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "%issue1",
		Description: "Issue description",
		Priority:    entities.IssuePriorities.Critical,
		Status:      entities.IssueStatuses.Open,
		AssigneeID:  utils.PtrFromValue(suite.users.Krosh.ID),
		MilestoneID: &milestone2.ID,
		LabelIDs:    []uint64{label1.ID, label2.ID},
		Visibility:  entities.IssueVisibilities.Public,
		Deadline:    &deadline,
	})
	issue2 := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "closed minor issue 2",
		Description: "Issue description",
		Priority:    entities.IssuePriorities.Minor,
		Status:      entities.IssueStatuses.Closed,
		AssigneeID:  utils.PtrFromValue(suite.users.Pikachu.ID),
		MilestoneID: nil,
		LabelIDs:    []uint64{label2.ID},
		Visibility:  entities.IssueVisibilities.Private,
	})
	issue3 := suite.makeIssue(suite.users.Pikachu, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "%Issue3",
		Description: "Issue description",
		Priority:    entities.IssuePriorities.Normal,
		Status:      entities.IssueStatuses.Open,
		AssigneeID:  utils.PtrFromValue(suite.users.Slowpoke.ID),
		MilestoneID: &milestone1.ID,
		LabelIDs:    []uint64{label1.ID},
		Visibility:  entities.IssueVisibilities.Public,
	})
	issue4 := suite.makeIssue(suite.users.Slowpoke, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "cloSED critical issue 4",
		Description: "Issue description",
		Priority:    entities.IssuePriorities.Critical,
		Status:      entities.IssueStatuses.Closed,
		AssigneeID:  nil,
		MilestoneID: &milestone1.ID,
		LabelIDs:    []uint64{label1.ID},
		Visibility:  entities.IssueVisibilities.Private,
		Deadline:    utils.PtrFromValue(deadline.Add(18 * time.Hour)),
	})
	issue5 := suite.makeIssue(suite.users.Pikachu, &makeIssueOptions{
		RepoID:      repoID,
		Title:       "issue5",
		Description: "Issue description",
		Priority:    entities.IssuePriorities.Trivial,
		Status:      entities.IssueStatuses.InProgress,
		AssigneeID:  utils.PtrFromValue(suite.users.Pikachu.ID),
		Visibility:  entities.IssueVisibilities.Public,
	})

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	err := suite.OpensearchKit.RefreshIndex(context.Background())
	require.NoError(t, err)
	return &issueFilterTest{
		orgSlug:    orgSlug,
		repoID:     repoID,
		repoSlug:   repoSlug,
		milestone1: milestone1,
		milestone2: milestone2,
		label1:     label1,
		label2:     label2,
		issue1:     issue1,
		issue2:     issue2,
		issue3:     issue3,
		issue4:     issue4,
		issue5:     issue5,
	}
}

func (suite *RwApiTestSuite) TestGrpcListIssuesWithFilter() {
	client := pb.NewIssueServiceClient(suite.grpcClient)
	f := prepareIssuesForFiltering(suite)
	repoID := f.repoID

	cases := map[string]struct {
		user    *entities.User
		request *pb.ListIssuesRequest
		code    codes.Code
	}{
		"filter by issue title": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "title",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: "%issue",
							},
						},
					},
				},
			},
			// Matches: 1, 3
		},
		"filter by issue title - ql": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				QlFilter: `title="%issue"`,
			},
			// Matches: 1, 3
		},
		"filter by closed title": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "title",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: "closed",
							},
						},
					},
				},
			},
			// Matches: 2, 4
		},
		"filter by priority": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "priority",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: pb.Issue_PRIORITY_CRITICAL.String(),
							},
						},
					},
				},
			},
			// Matches: 1, 4
		},
		"filter by status": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "status_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Open.ID),
							},
						},
					},
				},
			},
			// Matches: 1, 3
		},
		"filter by statuses": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
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
									Filter: &pagination_pb.Filter_Or{
										Or: &pagination_pb.Or{
											Operands: []*pagination_pb.Filter{
												{
													Filter: &pagination_pb.Filter_Predicate{
														Predicate: &pagination_pb.Predicate{
															Field:    "status_id",
															Operator: pagination_pb.Operator_OPERATOR_EQ,
															Operand: &pagination_pb.Predicate_StringValue{
																StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.InProgress.ID),
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 5
		},
		"filter by assignee": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "assignee_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(suite.users.Pikachu.ID),
							},
						},
					},
				},
			},
			// Matches: 2, 5
		},
		"filter by ne assignee": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "assignee_id",
							Operator: pagination_pb.Operator_OPERATOR_NE,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(suite.users.Pikachu.ID),
							},
						},
					},
				},
			},
			// Matches: 1, 3, 4
		},
		"filter by milestone": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "milestone_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(f.milestone1.ID),
							},
						},
					},
				},
			},
			// Matches: 3, 4
		},
		"filter by ne milestone": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "milestone_id",
							Operator: pagination_pb.Operator_OPERATOR_NE,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(f.milestone1.ID),
							},
						},
					},
				},
			},
			// Matches: 1, 2, 5
		},
		"filter by empty milestone": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "milestone_id",
							Operator: pagination_pb.Operator_OPERATOR_EMPTY,
						},
					},
				},
			},
			// Matches: 2, 5
		},
		"filter by label": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "label_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(f.label1.ID),
							},
						},
					},
				},
			},
			// Matches: 1, 3, 4
		},
		"filter by labels": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Or{
						Or: &pagination_pb.Or{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "label_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(f.label1.ID),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "label_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(f.label2.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 1, 2, 3, 4
		},

		"filter by ne labels": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "label_id",
											Operator: pagination_pb.Operator_OPERATOR_NE,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(f.label1.ID),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "label_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(f.label2.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 2
		},
		"filter by author": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "author_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(suite.users.Admin.ID),
							},
						},
					},
				},
			},
			// Matches: 1
		},
		"filter by visibility": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "visibility",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: pb.Issue_VISIBILITY_PRIVATE.String(),
							},
						},
					},
				},
			},
			// Matches: 2, 4
		},
		"filter by created_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "created_at",
							Operator: pagination_pb.Operator_OPERATOR_GT,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(f.issue1.CreatedAt),
							},
						},
					},
				},
			},
			// Matches: 2, 3, 4
		},
		"filter by updated_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "created_at",
							Operator: pagination_pb.Operator_OPERATOR_LTE,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(f.issue3.CreatedAt),
							},
						},
					},
				},
			},
			// Matches: 1, 2, 3
		},
		"filter by started_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "started_at",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(*f.issue5.StartedAt),
							},
						},
					},
				},
			},
			// Matches: 5
		},
		"filter by empty started_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "started_at",
							Operator: pagination_pb.Operator_OPERATOR_EMPTY,
						},
					},
				},
			},
			// Matches: 1, 2, 3, 4
		},
		"filter by completed_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "completed_at",
							Operator: pagination_pb.Operator_OPERATOR_LT,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(*f.issue4.CompletedAt),
							},
						},
					},
				},
			},
			// Matches: 2
		},
		"filter by empty completed_at": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "completed_at",
							Operator: pagination_pb.Operator_OPERATOR_EMPTY,
						},
					},
				},
			},
			// Matches: 1, 3, 5
		},
		"filter by deadline": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "deadline",
							Operator: pagination_pb.Operator_OPERATOR_LTE,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(*f.issue1.Deadline),
							},
						},
					},
				},
			},
			// Matches: 1
		},
		"filter by gt deadline": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "deadline",
							Operator: pagination_pb.Operator_OPERATOR_GT,
							Operand: &pagination_pb.Predicate_TimestampValue{
								TimestampValue: grpc.TimeToProtocTs(*f.issue1.Deadline),
							},
						},
					},
				},
			},
			// Matches: 4
		},
		"filter by priority and author": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "priority",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: pb.Issue_PRIORITY_CRITICAL.String(),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "author_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 4
		},
		"empty filter by admin": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{},
				},
			},
			// Matches: 1, 2, 3, 4, 5
		},
		"empty filter by krosh": {
			user: suite.users.Krosh,
			request: &pb.ListIssuesRequest{
				RepoId: grpc_marshalling.IDInverse(repoID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{},
				},
			},
			// Matches: 1, 2, 3, 5
		},
	}

	for _, useIndex := range []bool{false, true} {
		testSuffix := ""
		if useIndex {
			testSuffix = " with query"
		}
		for tn, tc := range cases {
			suite.T().Run(tn+testSuffix, func(t *testing.T) {
				if useIndex {
					suite.OpensearchBackendProxy.MakeAvaliable()
				}

				if useIndex {
					tc.request.Query = utils.PtrFromValue("description")
				}
				ctx := testutils.AuthorizeGRPC(tc.user.Identity)
				resp, err := client.List(ctx, tc.request)
				yarequire.ProtoStatusEqual(t, tc.code, err)
				if tc.code != codes.OK {
					return
				}

				// yarequire.ProtoDumpFixture(t, resp)
				yarequire.ProtoCompareWithFixture(t, resp,
					protocmp.IgnoreDefaultScalars(),
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
				)

				if useIndex {
					suite.OpensearchBackendProxy.MakeUnavaliable()
				}
			})
		}
	}
}
func (suite *RwApiTestSuite) TestGrpcListIssuesWithQL() {
	client := pb.NewIssueServiceClient(suite.grpcClient)
	f := prepareIssuesForFiltering(suite)
	repoID := f.repoID

	cases := map[string]struct {
		user    *entities.User
		request *pb.ListIssuesRequest
		code    codes.Code
	}{
		"filter by issue title": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				QlFilter: `title="%issue"`,
			},
			// Matches: 1, 3
		},
		"filter by priority": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				QlFilter: `priority=critical`,
			},
			// Matches: 1, 4
		},
		"filter by status": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				QlFilter: `status=open`,
			},
			// Matches: 1, 3
		},
		"complex filter": {
			user: suite.users.Admin,
			request: &pb.ListIssuesRequest{
				RepoId:   grpc_marshalling.IDInverse(repoID),
				QlFilter: `status=open and priority=critical`,
			},
			// Matches: 1
		},
	}

	for tn, tc := range cases {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.List(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
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
			)

		})
	}

	casesWithMultipleRequests := map[string]struct {
		user     *entities.User
		requests []*pb.ListIssuesRequest
		code     codes.Code
	}{
		"filter by author with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_id=\"%d\"", suite.users.Pikachu.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_uuid=\"%s\"", suite.users.Pikachu.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_slug=\"%s\"", suite.users.Pikachu.Username),
				},
			},
			// Matches: 3, 5
		},
		"filter by author with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_id!=\"%d\"", suite.users.Pikachu.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_uuid!=\"%s\"", suite.users.Pikachu.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("author_slug!=\"%s\"", suite.users.Pikachu.Username),
				},
			},
			// Matches: 1, 2, 4
		},
		"filter by assignee with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_id=\"%d\"", suite.users.Pikachu.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_uuid=\"%s\"", suite.users.Pikachu.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_slug=\"%s\"", suite.users.Pikachu.Username),
				},
			},
			// Matches: 2, 5
		},
		"filter by assignee with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_id!=\"%d\"", suite.users.Pikachu.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_uuid!=\"%s\"", suite.users.Pikachu.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("assignee_slug!=\"%s\"", suite.users.Pikachu.Username),
				},
			},
			// Matches: 1, 3, 4
		},
		"filter by milestone with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_id=\"%d\"", f.milestone1.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_uuid=\"%s\"", f.milestone1.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_slug=\"%s\"", *f.milestone1.Slug),
				},
			},
			// Matches: 3, 4
		},
		"filter by milestone with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_id!=\"%d\"", f.milestone1.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_uuid!=\"%s\"", f.milestone1.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("milestone_slug!=\"%s\"", *f.milestone1.Slug),
				},
			},
			// Matches: 1, 2, 5
		},
		"filter by label with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_id=\"%d\"", f.label1.ID),
				},
				// deprecated
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_ids=\"%d\"", f.label1.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_uuid=\"%s\"", f.label1.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_slug=\"%s\"", *f.label1.Slug),
				},
			},
			// Matches: 1, 3, 4
		},
		"filter by label with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_id!=\"%d\"", f.label1.ID),
				},
				// deprecated
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_ids!=\"%d\"", f.label1.ID),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_uuid!=\"%s\"", f.label1.UUID.String()),
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: fmt.Sprintf("label_slug!=\"%s\"", *f.label1.Slug),
				},
			},
			// Matches: 2, 5
		},
		"filter by unexisting slugs with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "author_slug=Eevee",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "assignee_slug=Eevee",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "milestone_slug=milestone3",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "label_slug=unknown_label",
				},
			},
			// Matches: <nil>
		},
		"filter by unexisting uuids with equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "author_uuid=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "assignee_uuid=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "milestone_uuid=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "label_uuid=\"01123581-3213-4558-9144-233377610987\"",
				},
			},
			// Matches: <nil>
		},
		"filter by unexisting slugs with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "author_slug!=Eevee",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "assignee_slug!=Eevee",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "milestone_slug!=milestone3",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "label_slug!=unknown_label",
				},
			},
			// Matches: 1, 2, 3, 4, 5
		},
		"filter by unexisting uuids with not equal": {
			user: suite.users.Admin,
			requests: []*pb.ListIssuesRequest{
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "author_uuid!=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "assignee_uuid!=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "milestone_uuid!=\"01123581-3213-4558-9144-233377610987\"",
				},
				{
					RepoId:   grpc_marshalling.IDInverse(repoID),
					QlFilter: "label_uuid!=\"01123581-3213-4558-9144-233377610987\"",
				},
			},
			// Matches: 1, 2, 3, 4, 5
		},
	}
	for tn, tc := range casesWithMultipleRequests {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			var responses []*pb.ListIssuesResponse
			for _, req := range tc.requests {
				resp, err := client.List(ctx, req)
				yarequire.ProtoStatusEqual(t, tc.code, err)
				if tc.code != codes.OK {
					return
				}
				responses = append(responses, resp)
			}

			// check that all responses are equal
			require.Greater(t, len(responses), 0)
			resp := responses[0]
			for _, elem := range responses[1:] {
				yarequire.ProtoEqual(t, elem, resp)
			}

			ingoreFiels := []cmp.Option{protocmp.IgnoreFields(
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
			}
			// yarequire.ProtoDumpFixture(t, resp, ingoreFiels...)
			yarequire.ProtoCompareWithFixture(t, resp, ingoreFiels...)
		})
	}
}
