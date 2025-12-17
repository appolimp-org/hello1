package integrationtests

import (
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcGetFilterState() {
	t := suite.T()
	client := pb.NewFilterStateServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	user := suite.users.Admin

	filter := &pagination.Filter{
		Filter: &pagination.Filter_And{
			And: &pagination.And{
				Operands: []*pagination.Filter{
					{
						Filter: &pagination.Filter_Predicate{
							Predicate: &pagination.Predicate{
								Field:    "priority",
								Operator: pagination.Operator_OPERATOR_EQ,
								Operand: &pagination.Predicate_StringValue{
									StringValue: pb.Issue_PRIORITY_CRITICAL.String(),
								},
							},
						},
					},
					{
						Filter: &pagination.Filter_Predicate{
							Predicate: &pagination.Predicate{
								Field:    "author_id",
								Operator: pagination.Operator_OPERATOR_EQ,
								Operand: &pagination.Predicate_StringValue{
									StringValue: grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.Update(testutils.AuthorizeGRPC(user.Identity), &pb.UpdateFilterStateRequest{
		Entity: &pb.EntityIdentity{
			Id:   grpc_marshalling.IDInverse(repoID),
			Type: pb.EntityIdentity_TYPE_REPOSITORY,
		},
		Scope:  pb.FilterStateScope_FILTER_STATE_SCOPE_ISSUES_LIST,
		Filter: filter,
	})
	require.NoError(t, err)

	invertedFilter := &pagination.Filter{
		Filter: &pagination.Filter_Not{
			Not: &pagination.Not{
				Operand: filter,
			},
		},
	}

	_, err = client.Update(testutils.AuthorizeGRPC(user.Identity), &pb.UpdateFilterStateRequest{
		Entity: &pb.EntityIdentity{
			Id:   grpc_marshalling.IDInverse(repoID),
			Type: pb.EntityIdentity_TYPE_REPOSITORY,
		},
		Scope:  pb.FilterStateScope_FILTER_STATE_SCOPE_PULL_REQUESTS_LIST,
		Filter: invertedFilter,
	})
	require.NoError(t, err)

	tt := map[string]struct {
		request        *pb.GetFilterStateRequest
		expectedFilter *pagination.Filter
		expectedStatus codes.Code
	}{
		"get issues list state": {
			request: &pb.GetFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: pb.FilterStateScope_FILTER_STATE_SCOPE_ISSUES_LIST,
			},
			expectedFilter: filter,
			expectedStatus: codes.OK,
		},
		"get prs list state": {
			request: &pb.GetFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: pb.FilterStateScope_FILTER_STATE_SCOPE_PULL_REQUESTS_LIST,
			},
			expectedFilter: invertedFilter,
			expectedStatus: codes.OK,
		},
		"another type": {
			request: &pb.GetFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_USER,
				},
				Scope: pb.FilterStateScope_FILTER_STATE_SCOPE_ISSUES_LIST,
			},
			expectedStatus: codes.NotFound,
		},
		"another id": {
			request: &pb.GetFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   "123456789",
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: pb.FilterStateScope_FILTER_STATE_SCOPE_ISSUES_LIST,
			},
			expectedStatus: codes.NotFound,
		},
	}

	for name, tc := range tt {
		suite.T().Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(user.Identity)
			resp, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			if tc.expectedStatus != codes.OK {
				return
			}
			yarequire.ProtoEqual(t, tc.expectedFilter, resp)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcUpdateFilterState() {
	client := pb.NewFilterStateServiceClient(suite.grpcClient)
	repoID := suite.repos.Alpha.ID
	user := suite.users.Admin
	issuesList := pb.FilterStateScope_FILTER_STATE_SCOPE_ISSUES_LIST

	tt := map[string]struct {
		request        *pb.UpdateFilterStateRequest
		expectedStatus codes.Code
	}{
		"initial update": {
			request: &pb.UpdateFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: issuesList,
				Filter: &pagination.Filter{
					Filter: &pagination.Filter_Predicate{
						Predicate: &pagination.Predicate{
							Field:    "status_id",
							Operator: pagination.Operator_OPERATOR_EQ,
							Operand: &pagination.Predicate_StringValue{
								StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Open.ID),
							},
						},
					},
				},
			},
			expectedStatus: codes.OK,
		},
		"update state": {
			request: &pb.UpdateFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: issuesList,
				Filter: &pagination.Filter{
					Filter: &pagination.Filter_And{
						And: &pagination.And{
							Operands: []*pagination.Filter{
								{
									Filter: &pagination.Filter_Predicate{
										Predicate: &pagination.Predicate{
											Field:    "status_id",
											Operator: pagination.Operator_OPERATOR_NE,
											Operand: &pagination.Predicate_StringValue{
												StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Closed.ID),
											},
										},
									},
								},
								{
									Filter: &pagination.Filter_Or{
										Or: &pagination.Or{
											Operands: []*pagination.Filter{
												{
													Filter: &pagination.Filter_Predicate{
														Predicate: &pagination.Predicate{
															Field:    "status_id",
															Operator: pagination.Operator_OPERATOR_EQ,
															Operand: &pagination.Predicate_StringValue{
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
			expectedStatus: codes.OK,
		},
		"invalid state": {
			request: &pb.UpdateFilterStateRequest{
				Entity: &pb.EntityIdentity{
					Id:   grpc_marshalling.IDInverse(repoID),
					Type: pb.EntityIdentity_TYPE_REPOSITORY,
				},
				Scope: issuesList,
				Filter: &pagination.Filter{
					Filter: &pagination.Filter_Predicate{
						Predicate: &pagination.Predicate{
							Field:    "imaginary_field",
							Operator: pagination.Operator_OPERATOR_LT, // imaginary_field not exists
							Operand: &pagination.Predicate_StringValue{
								StringValue: grpc_marshalling.ShortIDInverse(entities.IssueStatuses.Open.ID),
							},
						},
					},
				},
			},
			expectedStatus: codes.InvalidArgument,
		},
	}

	for name, tc := range tt {
		suite.T().Run(name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(user.Identity)
			updResp, updErr := client.Update(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, updErr)
			if tc.expectedStatus != codes.OK {
				return
			}
			yarequire.ProtoEqual(t, tc.request.Filter, updResp)

			getResp, getErr := client.Get(ctx, &pb.GetFilterStateRequest{
				Entity: tc.request.Entity,
				Scope:  tc.request.Scope,
			})
			require.NoError(t, getErr)
			yarequire.ProtoEqual(t, tc.request.Filter, getResp)
		})
	}
}
