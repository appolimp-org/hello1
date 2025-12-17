package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcUpdateLinkedPullRequests() {
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)
	prClient := pb.NewPRServiceClient(suite.grpcClient)

	pr1 := suite.makePullRequest(suite.users.Admin, nil)
	pr2 := suite.makePullRequest(suite.users.Admin, nil)

	createDefaultIssue := func() uint64 {
		return suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     suite.repos.Alpha.ID,
			Title:      "Default Issue",
			Visibility: entities.IssueVisibilities.Public,
		}).ID
	}

	tt := map[string]struct {
		initialRequest *pb.UpdateLinkedPullRequestsRequest
		updateRequest  *pb.UpdateLinkedPullRequestsRequest
		user           *entities.User
		checkIssue     func(*testing.T, *entities.User, *pb.Issue)
		expectedStatus codes.Code
	}{
		"add linked pull request": {
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, user *entities.User, issue *pb.Issue) {
				require.Equal(t, []string{
					grpc_marshalling.IDInverse(pr1.ID),
				}, functools.Map(issue.LinkedPrs, (*pb.LinkedPullRequest).GetId))
			},
		},
		"remove linked pull request": {
			initialRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, user *entities.User, issue *pb.Issue) {
				require.Empty(t, issue.LinkedPrs)
			},
		},
		"add and remove same pull request": {
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, user *entities.User, issue *pb.Issue) {
				require.Empty(t, issue.LinkedPrs)
			},
		},
		"add multiple pull requests": {
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(pr2.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkIssue: func(t *testing.T, user *entities.User, issue *pb.Issue) {
				require.ElementsMatch(t, []string{
					grpc_marshalling.IDInverse(pr1.ID),
					grpc_marshalling.IDInverse(pr2.ID),
				}, []string{
					issue.LinkedPrs[0].Id,
					issue.LinkedPrs[1].Id,
				})
			},
		},
		"remove non-existing pull request": {
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     "123456789",
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.PermissionDenied,
		},
		"permission denied": {
			updateRequest: &pb.UpdateLinkedPullRequestsRequest{
				PrDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(pr1.ID),
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
			issueID := createDefaultIssue()

			if tc.initialRequest != nil {
				tc.initialRequest.Id = grpc_marshalling.IDInverse(issueID)
				_, err := issueClient.UpdateLinkedPullRequests(ctx, tc.initialRequest)
				yarequire.ProtoStatusEqual(t, codes.OK, err)
			}

			tc.updateRequest.Id = grpc_marshalling.IDInverse(issueID)
			resp, err := issueClient.UpdateLinkedPullRequests(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			updatedIssue, err := grpc_marshalling.OperationResponse(resp, &pb.Issue{})
			require.NoError(t, err)
			if tc.checkIssue != nil {
				tc.checkIssue(t, tc.user, updatedIssue)
			}

			// check prs
			linkedPRs := functools.Map(updatedIssue.LinkedPrs, (*pb.LinkedPullRequest).GetId)
			for _, prID := range linkedPRs {
				pr, err := prClient.Get(ctx, &pb.GetPullRequestRequest{
					Identity: &pb.GetPullRequestRequest_Id{
						Id: prID,
					},
				})
				require.NoError(t, err)
				linkedIssues := functools.Map(pr.LinkedIssues, (*pb.LinkedIssue).GetId)
				require.Contains(t, linkedIssues, grpc_marshalling.IDInverse(issueID))
			}
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateLinkedIssues() {
	prClient := pb.NewPRServiceClient(suite.grpcClient)
	issueClient := pb.NewIssueServiceClient(suite.grpcClient)

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Issue1",
		Visibility: entities.IssueVisibilities.Private,
	})
	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Issue2",
		Visibility: entities.IssueVisibilities.Public,
	})

	createDefaultPullRequest := func() uint64 {
		return suite.makePullRequest(suite.users.Admin, nil).ID
	}

	tt := map[string]struct {
		initialRequest   *pb.UpdateLinkedIssuesRequest
		updateRequest    *pb.UpdateLinkedIssuesRequest
		user             *entities.User
		checkPullRequest func(*testing.T, *entities.User, *pb.PullRequest)
		expectedStatus   codes.Code
	}{
		"add linked issue": {
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkPullRequest: func(t *testing.T, user *entities.User, pr *pb.PullRequest) {
				switch user {
				case suite.users.Admin:
					require.Equal(t, []string{
						grpc_marshalling.IDInverse(issue1.ID),
					}, functools.Map(pr.LinkedIssues, (*pb.LinkedIssue).GetId))
				default:
					require.Equal(t, []string{}, functools.Map(pr.LinkedIssues, (*pb.LinkedIssue).GetId))
				}
			},
		},
		"remove linked issue": {
			initialRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkPullRequest: func(t *testing.T, user *entities.User, pr *pb.PullRequest) {
				require.Empty(t, pr.LinkedIssues)
			},
		},
		"add and remove same issue": {
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkPullRequest: func(t *testing.T, user *entities.User, pr *pb.PullRequest) {
				require.Empty(t, pr.LinkedIssues)
			},
		},
		"add multiple issues": {
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						Id:     grpc_marshalling.IDInverse(issue2.ID),
						Action: pb.DeltaAction_ADD,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.OK,
			checkPullRequest: func(t *testing.T, user *entities.User, pr *pb.PullRequest) {
				switch user {
				case suite.users.Admin:
					require.ElementsMatch(t, []string{
						grpc_marshalling.IDInverse(issue1.ID),
						grpc_marshalling.IDInverse(issue2.ID),
					}, []string{
						pr.LinkedIssues[0].Id,
						pr.LinkedIssues[1].Id,
					})
				default:
					require.ElementsMatch(t, []string{
						grpc_marshalling.IDInverse(issue2.ID),
					}, []string{
						pr.LinkedIssues[0].Id,
					})
				}
			},
		},
		"remove non-existing issue": {
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     "123456789",
						Action: pb.DeltaAction_REMOVE,
					},
				},
			},
			user:           suite.users.Admin,
			expectedStatus: codes.PermissionDenied,
		},
		"permission denied": {
			updateRequest: &pb.UpdateLinkedIssuesRequest{
				IssueDeltas: []*pb.IdDelta{
					{
						Id:     grpc_marshalling.IDInverse(issue1.ID),
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
			prID := createDefaultPullRequest()

			if tc.initialRequest != nil {
				tc.initialRequest.Id = grpc_marshalling.IDInverse(prID)
				_, err := prClient.UpdateLinkedIssues(ctx, tc.initialRequest)
				yarequire.ProtoStatusEqual(t, codes.OK, err)
			}

			tc.updateRequest.Id = grpc_marshalling.IDInverse(prID)
			resp, err := prClient.UpdateLinkedIssues(ctx, tc.updateRequest)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}

			updatedPullRequest, err := grpc_marshalling.OperationResponse(resp, &pb.PullRequest{})
			require.NoError(t, err)
			if tc.checkPullRequest != nil {
				tc.checkPullRequest(t, tc.user, updatedPullRequest)
			}

			fetchedPullRequest, err := prClient.Get(testutils.AuthorizeGRPC(suite.users.Krosh.Identity), &pb.GetPullRequestRequest{
				Identity: &pb.GetPullRequestRequest_Id{Id: grpc_marshalling.IDInverse(prID)},
			})
			require.NoError(t, err)
			if tc.checkPullRequest != nil {
				tc.checkPullRequest(t, suite.users.Krosh, fetchedPullRequest)
			}

			// check issues
			linkedIssues := functools.Map(updatedPullRequest.LinkedIssues, (*pb.LinkedIssue).GetId)
			for _, issueID := range linkedIssues {
				issue, err := issueClient.Get(ctx, &pb.GetIssueRequest{
					Issue: &pb.GetIssueRequest_Id{
						Id: issueID,
					},
				})
				require.NoError(t, err)
				linkedPRs := functools.Map(issue.LinkedPrs, (*pb.LinkedPullRequest).GetId)
				require.Contains(t, linkedPRs, grpc_marshalling.IDInverse(prID))
			}
		})
	}
}
