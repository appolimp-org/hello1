package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcListUserPullRequests() {
	t := suite.T()

	client := pb.NewPRServiceClient(suite.grpcClient)

	suite.makePullRequest(suite.users.Barash, &makePrOptions{Title: "1", Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Title: "2", Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Title: "3", Reviewers: []uint64{suite.users.Barash.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Title: "4", Reviewers: []uint64{}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.AuthRepoPrivate, Title: "6", Reviewers: []uint64{suite.users.Barash.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:       suite.repos.Alpha,
		SourceRepo: suite.repos.AlphaFork,
		Title:      "7",
		Source:     "master",
		Target:     "master",
		Reviewers:  []uint64{suite.users.Barash.ID},
	})

	// Create PRs in a public repo for anonymous user access tests
	suite.makePullRequest(suite.users.PinPublic, &makePrOptions{Repo: suite.repos.AuthRepoPublic, Title: "8 - public author", Reviewers: []uint64{suite.users.Krosh.ID}})
	suite.makePullRequest(suite.users.PinPublic, &makePrOptions{Repo: suite.repos.AuthRepoPublic, Title: "9 - public author", Reviewers: []uint64{suite.users.Slowpoke.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.AuthRepoPublic, Title: "10 - public reviewer", Reviewers: []uint64{suite.users.PinPublic.ID}})

	cases := []struct {
		name    string
		user    *entities.User
		request *pb.ListUserPullRequestsRequest
		code    codes.Code
	}{
		{
			name: "happy path, author",
			user: suite.users.Admin,
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.Barash.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_AUTHOR,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 1 & 2
		},
		{
			name: "happy path, reviewer",
			user: suite.users.Admin,
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.Barash.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_REVIEWER,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 3 & 6 & 7
		},
		{
			name: "empty",
			user: suite.users.Admin,
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.Raichu.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_REVIEWER,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// nothing
		},
		{
			name: "access restrictions, reviewer",
			user: suite.users.Barash,
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.Barash.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_REVIEWER,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 3
		},
		{
			name: "anonymous user, list public user PRs as author",
			user: entities.NewAnonymousUser(),
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.PinPublic.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_AUTHOR,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 8 & 9 (only PRs in public repos)
		},
		{
			name: "anonymous user, list public user PRs as reviewer",
			user: entities.NewAnonymousUser(),
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.PinPublic.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_REVIEWER,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 10 & 2 (only PRs in public repos)
		},
		{
			name: "anonymous user, list public user PRs with ANY role",
			user: entities.NewAnonymousUser(),
			request: &pb.ListUserPullRequestsRequest{
				UserId: grpc.MarshalID(suite.users.PinPublic.ID),
				Role:   pb.ListUserPullRequestsRequest_ROLE_ANY,
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 10, 2, 8, 9 (all PRs involving PinPublic in public repos)
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListUserPullRequests(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.PullRequestSummary{}, "id", "public_id", "created_at", "updated_at", "repo_id", "fork_repo_id"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcListRepoPullRequests() {
	client := pb.NewPRServiceClient(suite.grpcClient)

	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "1 - alpha", Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID}})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{Repo: suite.repos.Alpha, Title: "2 - alpha", Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.Alpha, Title: "3 - alpha", Reviewers: []uint64{suite.users.Barash.ID}})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{Repo: suite.repos.BigDiff, Target: "main", Source: "feature", Title: "4 - big diff", Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID}})
	suite.makePullRequest(suite.users.Pikachu, &makePrOptions{
		Repo:      suite.repos.BigDiff,
		Target:    "main",
		Source:    "feature",
		Title:     "5 - big diff",
		Reviewers: []uint64{suite.users.Barash.ID, suite.users.PinPublic.ID, suite.users.Slowpoke.ID},
	})

	cases := map[string]struct {
		user    *entities.User
		request *pb.ListRepoPullRequestsRequest
		code    codes.Code
	}{
		"happy path, repo1": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 1 & 2 & 3
		},
		"happy path, repo2": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.BigDiff.ID),
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// 4 & 5
		},
		"empty": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Crisscross.ID),
				SortBy: []*pagination_pb.SortOption{{Column: "title", Direction: pagination_pb.SortOption_ASC}}, // adds determinism
			},
			// nothing
		},
	}

	for tn, tc := range cases {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListRepoPullRequests(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.PullRequestSummary{}, "id", "created_at", "updated_at"), // random each time
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"),
			)
		})
	}

	suite.T().Run("pagination", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
		request := &pb.ListRepoPullRequestsRequest{
			RepoId:   grpc.MarshalID(suite.repos.Alpha.ID),
			SortBy:   []*pagination_pb.SortOption{{Column: "created_at", Direction: pagination_pb.SortOption_ASC}},
			PageSize: utils.PtrFromValue(uint64(1)),
		}

		var prs []*pb.PullRequestSummary
		for i := 0; i < 3; i++ {
			resp, err := client.ListRepoPullRequests(ctx, request)
			require.NoError(t, err)
			require.Len(t, resp.PullRequests, 1)

			prs = append(prs, resp.PullRequests...)
			if resp.NextPageToken == "" {
				break
			}

			request.SortBy = nil
			request.PageToken = &resp.NextPageToken
		}

		require.Len(t, prs, 3)
	})
}

func (suite *RwApiTestSuite) TestGrpcListRepoPullRequestsWithFilter() {
	client := pb.NewPRServiceClient(suite.grpcClient)

	suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "PR1",
		Reviewers: []uint64{suite.users.Krosh.ID, suite.users.Pikachu.ID},
		Publish:   utils.PtrFromValue(true),
		Source:    "branch",
		Target:    "master",
	})
	suite.makePullRequest(suite.users.Barash, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "PR2",
		Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID},
		Publish:   utils.PtrFromValue(false),
		Source:    "master",
		Target:    "branch",
	})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "PR3",
		Reviewers: []uint64{suite.users.Barash.ID},
		Publish:   utils.PtrFromValue(true),
		Source:    "branch",
		Target:    "master",
	})
	suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "PR4",
		Reviewers: []uint64{suite.users.PinPublic.ID, suite.users.Slowpoke.ID},
		Publish:   utils.PtrFromValue(false),
		Source:    "branch",
		Target:    "master",
	})
	suite.makePullRequest(suite.users.Pikachu, &makePrOptions{
		Repo:      suite.repos.Alpha,
		Title:     "PR5",
		Reviewers: []uint64{suite.users.Barash.ID, suite.users.PinPublic.ID, suite.users.Slowpoke.ID},
		Publish:   utils.PtrFromValue(true),
		Source:    "master",
		Target:    "branch",
	})

	cases := map[string]struct {
		user    *entities.User
		request *pb.ListRepoPullRequestsRequest
		code    codes.Code
	}{
		"filter by author": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "author_id",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: grpc_marshalling.IDInverse(suite.users.Barash.ID),
							},
						},
					},
				},
			},
			// Matches: 1, 2
		},
		"filter by not existed author": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
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
			// Matches: nothing
		},
		"filter authors by and": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "author_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(suite.users.Barash.ID),
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
												StringValue: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: nothing (author cannot be two IDs at once)
		},
		"filter authors by or": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Or{
						Or: &pagination_pb.Or{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "author_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(suite.users.Barash.ID),
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
												StringValue: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
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
		"filter by status open": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "pr_status",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: pb.PullRequest_Status_name[int32(pb.PullRequest_OPEN)],
							},
						},
					},
				},
			},
			// Matches: 1, 3, 5
		},
		"filter by status draft": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "pr_status",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: pb.PullRequest_Status_name[int32(pb.PullRequest_DRAFT)],
							},
						},
					},
				},
			},
			// Matches: 2, 4
		},
		"filter by not existed status": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "pr_status",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: "not_existed_status",
							},
						},
					},
				},
			},
			code: codes.InvalidArgument,
		},
		"filter by status open or draft": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Or{
						Or: &pagination_pb.Or{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "pr_status",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: pb.PullRequest_Status_name[int32(pb.PullRequest_OPEN)],
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "pr_status",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: pb.PullRequest_Status_name[int32(pb.PullRequest_DRAFT)],
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 1, 2, 3, 4, 5 (all)
		},
		"filter by author and status": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_And{
						And: &pagination_pb.And{
							Operands: []*pagination_pb.Filter{
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "author_id",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: grpc_marshalling.IDInverse(suite.users.Barash.ID),
											},
										},
									},
								},
								{
									Filter: &pagination_pb.Filter_Predicate{
										Predicate: &pagination_pb.Predicate{
											Field:    "pr_status",
											Operator: pagination_pb.Operator_OPERATOR_EQ,
											Operand: &pagination_pb.Predicate_StringValue{
												StringValue: pb.PullRequest_Status_name[int32(pb.PullRequest_OPEN)],
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Matches: 1
		},
		"filter by source branch": {
			user: suite.users.Admin,
			request: &pb.ListRepoPullRequestsRequest{
				RepoId: grpc.MarshalID(suite.repos.Alpha.ID),
				Filter: &pagination_pb.Filter{
					Filter: &pagination_pb.Filter_Predicate{
						Predicate: &pagination_pb.Predicate{
							Field:    "source_branch",
							Operator: pagination_pb.Operator_OPERATOR_EQ,
							Operand: &pagination_pb.Predicate_StringValue{
								StringValue: "branch",
							},
						},
					},
				},
			},
			// Matches: 1, 3, 4
		},
	}

	for tn, tc := range cases {
		suite.T().Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.ListRepoPullRequests(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.PullRequestSummary{}, "id", "public_id", "created_at", "updated_at"),
				protocmp.IgnoreFields(&pb.PRReviewer{}, "created_at", "updated_at"),
			)
		})
	}
}
