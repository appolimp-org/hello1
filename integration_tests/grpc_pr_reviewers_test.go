package integrationtests

import (
	"common/functools"
	"common/oyaml"
	"context"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"

	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/consts"
	"gitcore/internal/entities"
	"gitcore/internal/entities/signals"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestReviewersHandler_List() {
	t := suite.T()

	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	emptyPr, _ := suite.makePullRequestAndReviewers(t, 0)
	pr, prUsers := suite.makePullRequestAndReviewers(t, 3)

	type args struct {
		ctx     context.Context
		request *pb.ListReviewersRequest
	}
	tests := []struct {
		name     string
		args     args
		want     *pb.ListReviewersResponse
		wantCode codes.Code
	}{
		{
			name: "empty",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.ListReviewersRequest{
					PrId: grpc2.MarshalID(emptyPr.ID),
				},
			},
			want: &pb.ListReviewersResponse{
				Reviewers: []*pb.PRReviewer{},
				Revision: &pb.Revision{
					Value: "1",
					Count: utils.PtrFromValue(int32(0)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "ok",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.ListReviewersRequest{
					PrId: grpc2.MarshalID(pr.ID),
				},
			},
			want: &pb.ListReviewersResponse{
				Reviewers: []*pb.PRReviewer{
					{
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
					{
						UserId: grpc2.MarshalID(prUsers[1].ID),
					},
					{
						UserId: grpc2.MarshalID(prUsers[2].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "limit",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.ListReviewersRequest{
					PrId:     grpc2.MarshalID(pr.ID),
					PageSize: utils.PtrFromValue(uint64(2)),
				},
			},
			want: &pb.ListReviewersResponse{
				Reviewers: []*pb.PRReviewer{
					{
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
					{
						UserId: grpc2.MarshalID(prUsers[1].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(3)),
				},
				NextPageToken: testutils.Presence,
			},
			wantCode: codes.OK,
		},
		{
			name: "sort",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.ListReviewersRequest{
					PrId: grpc2.MarshalID(pr.ID),
					SortBy: []*pagination_pb.SortOption{
						{
							Column:    "created_at",
							Direction: pagination_pb.SortOption_DESC,
						},
					},
				},
			},
			want: &pb.ListReviewersResponse{
				Reviewers: []*pb.PRReviewer{
					{
						UserId: grpc2.MarshalID(prUsers[2].ID),
					},
					{
						UserId: grpc2.MarshalID(prUsers[1].ID),
					},
					{
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "pr_not_found",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.ListReviewersRequest{
					PrId: "123123",
				},
			},
			wantCode: codes.NotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.List(tt.args.ctx, tt.args.request)
			yarequire.ProtoStatusEqual(t, tt.wantCode, err)

			if tt.want == nil {
				require.Nil(t, resp)
				return
			}

			require.Len(t, resp.Reviewers, len(tt.want.Reviewers))
			for i := range resp.Reviewers {
				require.NotEmpty(t, resp.Reviewers[i].CreatedAt)
				require.NotEmpty(t, resp.Reviewers[i].UpdatedAt)

				resp.Reviewers[i].CreatedAt = tt.want.Reviewers[i].CreatedAt
				resp.Reviewers[i].UpdatedAt = tt.want.Reviewers[i].UpdatedAt
			}

			if tt.want.NextPageToken == testutils.Presence {
				require.NotEmpty(t, resp.NextPageToken)
				tt.want.NextPageToken = resp.NextPageToken
			}

			yarequire.ProtoEqual(t, tt.want, resp)
		})
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SetReviewers() {
	t := suite.T()

	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	someUsers := []*entities.User{
		suite.RandomUserFixture(),
		suite.RandomUserFixture(),
		suite.RandomUserFixture(),
	}

	emptyPr, _ := suite.makePullRequestAndReviewers(t, 0)
	pr, prUsers := suite.makePullRequestAndReviewers(t, 2)
	prToEmpty, prToEmptyUsers := suite.makePullRequestAndReviewers(t, 2)
	prAdd, prAddUsers := suite.makePullRequestAndReviewers(t, 2)

	type args struct {
		ctx     context.Context
		request *pb.SetReviewersRequest
	}
	tests := []struct {
		name     string
		args     args
		want     *pb.ReviewersOperationResult
		wantCode codes.Code
	}{
		{
			name: "unknown users",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId:                grpc2.MarshalID(emptyPr.ID),
					UserIds:             []string{"11111111", "2222222"},
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "same_empty",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId:                grpc2.MarshalID(emptyPr.ID),
					UserIds:             []string{},
					Revision:            utils.PtrFromValue("1"),
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{},
				Revision: &pb.Revision{
					Value: "1",
					Count: utils.PtrFromValue(int32(0)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "same_empty:wrong_revision",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId:                grpc2.MarshalID(emptyPr.ID),
					UserIds:             []string{},
					Revision:            utils.PtrFromValue("some_value"),
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name: "empty",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId: grpc2.MarshalID(emptyPr.ID),
					UserIds: []string{
						grpc2.MarshalID(someUsers[0].ID),
						grpc2.MarshalID(someUsers[1].ID),
						grpc2.MarshalID(someUsers[2].ID),
					},
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						UserId: grpc2.MarshalID(someUsers[0].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(someUsers[1].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(someUsers[2].ID),
						Action: pb.DeltaAction_ADD,
					},
				},
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "same_pr",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId: grpc2.MarshalID(pr.ID),
					UserIds: []string{
						grpc2.MarshalID(prUsers[0].ID),
						grpc2.MarshalID(prUsers[1].ID),
					},
					Revision:            utils.PtrFromValue("3"),
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{},
				Revision: &pb.Revision{
					Value: "3",
					Count: utils.PtrFromValue(int32(2)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "rewrite",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId: grpc2.MarshalID(pr.ID),
					UserIds: []string{
						grpc2.MarshalID(someUsers[0].ID),
						grpc2.MarshalID(someUsers[1].ID),
						grpc2.MarshalID(someUsers[2].ID),
					},
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						UserId: grpc2.MarshalID(someUsers[0].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(someUsers[1].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(someUsers[2].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(prUsers[0].ID),
						Action: pb.DeltaAction_REMOVE,
					},
					{
						UserId: grpc2.MarshalID(prUsers[1].ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "add_to_current",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId: grpc2.MarshalID(prAdd.ID),
					UserIds: []string{
						grpc2.MarshalID(prAddUsers[0].ID),
						grpc2.MarshalID(prAddUsers[1].ID),
						grpc2.MarshalID(someUsers[0].ID),
						grpc2.MarshalID(someUsers[1].ID),
					},
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						UserId: grpc2.MarshalID(someUsers[0].ID),
						Action: pb.DeltaAction_ADD,
					},
					{
						UserId: grpc2.MarshalID(someUsers[1].ID),
						Action: pb.DeltaAction_ADD,
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(4)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "set empty",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId:                grpc2.MarshalID(prToEmpty.ID),
					UserIds:             nil,
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						UserId: grpc2.MarshalID(prToEmptyUsers[0].ID),
						Action: pb.DeltaAction_REMOVE,
					},
					{
						UserId: grpc2.MarshalID(prToEmptyUsers[1].ID),
						Action: pb.DeltaAction_REMOVE,
					},
				},
				Revision: &pb.Revision{
					Value: "4",
					Count: utils.PtrFromValue(int32(0)),
				},
			},
			wantCode: codes.OK,
		},
		{
			name: "pr_not_found",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId:                "123123",
					UserIds:             []string{"123123"},
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "wrong revision",
			args: args{
				ctx: testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
				request: &pb.SetReviewersRequest{
					PrId: grpc2.MarshalID(pr.ID),
					UserIds: []string{
						grpc2.MarshalID(someUsers[0].ID),
					},
					Revision:            utils.PtrFromValue("some_value"),
					NotificationOptions: testutils.SkipNotificationPb,
				},
			},
			wantCode: codes.FailedPrecondition,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, err := client.Set(tt.args.ctx, tt.args.request)
			yarequire.ProtoStatusEqual(t, tt.wantCode, err)

			if tt.want == nil {
				require.Nil(t, op)
				return
			}

			// meta
			meta, err := op.Metadata.UnmarshalNew()
			require.NoError(t, err)

			yarequire.ProtoEqual(t, &pb.SetReviewersMetadata{
				PrId:    tt.args.request.PrId,
				UserIds: tt.args.request.UserIds,
			}, meta)

			// result
			resp := testutils.UnmarshalGrpcResult[*pb.ReviewersOperationResult](t, op)
			slices.SortFunc(resp.EffectiveDeltas, func(a, b *pb.ReviewerDelta) int {
				return strings.Compare(a.UserId, b.UserId)
			})
			yarequire.ProtoEqual(t, tt.want, resp)

			// check len
			respList, err := client.List(tt.args.ctx, &pb.ListReviewersRequest{
				PrId: tt.args.request.PrId,
			})
			require.NoError(t, err)
			yarequire.ProtoEqual(t, resp.Revision, respList.Revision)
		})
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_UpdateReviewers() {
	t := suite.T()

	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	someUsers := []*entities.User{
		suite.RandomUserFixture(),
		suite.RandomUserFixture(),
		suite.RandomUserFixture(),
	}
	pr, prUsers := suite.makePullRequestAndReviewers(t, 3)

	tests := []struct {
		name     string
		request  *pb.UpdateReviewersRequest
		want     *pb.ReviewersOperationResult
		wantIDs  []uint64
		wantCode codes.Code
	}{
		{
			name: "add 2, add 1 repeated",
			request: &pb.UpdateReviewersRequest{
				PrId: grpc2.MarshalID(pr.ID),
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[0].ID),
					},
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[1].ID),
					},
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
				},
				Revision:            utils.PtrFromValue("4"),
				NotificationOptions: testutils.SkipNotificationPb,
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[0].ID),
					},
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[1].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "5",
					Count: utils.PtrFromValue(int32(5)),
				},
			},
			wantIDs:  []uint64{prUsers[0].ID, prUsers[1].ID, prUsers[2].ID, someUsers[0].ID, someUsers[1].ID},
			wantCode: codes.OK,
		},
		{
			name: "remove 2, remove 1 non-present",
			request: &pb.UpdateReviewersRequest{
				PrId: grpc2.MarshalID(pr.ID),
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[1].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(someUsers[2].ID),
					},
				},
				Revision:            utils.PtrFromValue("5"),
				NotificationOptions: testutils.SkipNotificationPb,
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[1].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "6",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantIDs:  []uint64{prUsers[2].ID, someUsers[0].ID, someUsers[1].ID},
			wantCode: codes.OK,
		},
		{
			name: "add 1, remove 1",
			request: &pb.UpdateReviewersRequest{
				PrId: grpc2.MarshalID(pr.ID),
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[2].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[2].ID),
					},
				},
				Revision:            utils.PtrFromValue("6"),
				NotificationOptions: testutils.SkipNotificationPb,
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[2].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[2].ID),
					},
				},
				Revision: &pb.Revision{
					Value: "7",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantIDs:  []uint64{someUsers[0].ID, someUsers[1].ID, someUsers[2].ID},
			wantCode: codes.OK,
		},
		{
			name: "no effective deltas",
			request: &pb.UpdateReviewersRequest{
				PrId: grpc2.MarshalID(pr.ID),
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(someUsers[0].ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(prUsers[0].ID),
					},
				},
				Revision:            utils.PtrFromValue("7"),
				NotificationOptions: testutils.SkipNotificationPb,
			},
			want: &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{},
				Revision: &pb.Revision{
					Value: "7",
					Count: utils.PtrFromValue(int32(3)),
				},
			},
			wantIDs:  []uint64{someUsers[0].ID, someUsers[1].ID, someUsers[2].ID},
			wantCode: codes.OK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
			op, err := client.Update(ctx, tt.request)
			yarequire.ProtoStatusEqual(t, tt.wantCode, err)

			if tt.want == nil {
				require.Nil(t, op)
				return
			}

			// meta
			meta, err := op.Metadata.UnmarshalNew()
			require.NoError(t, err)
			typedMeta, ok := meta.(*pb.UpdateReviewersMetadata)
			require.True(t, ok, "correct metadata type")

			wantIDs := functools.Map(tt.wantIDs, grpc2.MarshalID)
			slices.Sort(wantIDs)
			slices.Sort(typedMeta.UserIds)
			yarequire.ProtoEqual(t, &pb.UpdateReviewersMetadata{
				PrId:    tt.request.PrId,
				UserIds: wantIDs,
			}, typedMeta)

			// result
			resp := testutils.UnmarshalGrpcResult[*pb.ReviewersOperationResult](t, op)
			slices.SortFunc(resp.EffectiveDeltas, func(a, b *pb.ReviewerDelta) int {
				return strings.Compare(a.UserId, b.UserId)
			})
			yarequire.ProtoEqual(t, tt.want, resp)

			// check len
			respList, err := client.List(ctx, &pb.ListReviewersRequest{
				PrId: tt.request.PrId,
			})
			require.NoError(t, err)
			yarequire.ProtoEqual(t, resp.Revision, respList.Revision)
		})
	}
}

func (suite *RwApiTestSuite) TestPR_SetReviewDecision() {
	t := suite.T()
	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	pr, prUsers := suite.makePullRequestAndReviewers(t, 3)
	prProtoID := grpc2.MarshalID(pr.ID)

	for _, test := range []struct {
		name                     string
		rIdx                     int
		decision                 *pb.ReviewDecision
		approves, trusts, blocks uint64
	}{
		{
			name:     "approve",
			rIdx:     0,
			decision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
			approves: 1,
			trusts:   0,
			blocks:   0,
		},
		{
			name:     "approve to trust",
			rIdx:     0,
			decision: utils.PtrFromValue(pb.ReviewDecision_RD_TRUST),
			approves: 0,
			trusts:   1,
			blocks:   0,
		},
		{
			name:     "second approve",
			rIdx:     1,
			decision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
			approves: 1,
			trusts:   1,
			blocks:   0,
		},
		{
			name:     "block",
			rIdx:     2,
			decision: utils.PtrFromValue(pb.ReviewDecision_RD_BLOCK),
			approves: 1,
			trusts:   1,
			blocks:   1,
		},
		{
			name:     "remove approve",
			rIdx:     1,
			decision: nil,
			approves: 0,
			trusts:   1,
			blocks:   1,
		},
		{
			name:     "remove trust",
			rIdx:     0,
			decision: nil,
			approves: 0,
			trusts:   0,
			blocks:   1,
		},
		{
			name:     "block to abstain",
			rIdx:     2,
			decision: utils.PtrFromValue(pb.ReviewDecision_RD_ABSTAIN),
			approves: 0,
			trusts:   0,
			blocks:   0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := prUsers[test.rIdx]
			suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
			ctx := testutils.AuthorizeGRPC(user.Identity)
			_, err := client.SetDecision(ctx, &pb.SetDecisionRequest{
				PrId:           prProtoID,
				ReviewDecision: test.decision,
			})
			require.NoError(t, err)

			ctx = testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
			response, err := client.List(ctx, &pb.ListReviewersRequest{
				PrId:     prProtoID,
				PageSize: utils.PtrFromValue(uint64(3)),
			})
			require.NoError(t, err)

			var ac, tc, bc uint64
			for _, review := range response.Reviewers {
				if review.ReviewDecision == nil {
					continue
				}
				switch *review.ReviewDecision {
				case pb.ReviewDecision_RD_APPROVE:
					ac++
				case pb.ReviewDecision_RD_TRUST:
					tc++
				case pb.ReviewDecision_RD_BLOCK:
					bc++
				case pb.ReviewDecision_RD_UNSPECIFIED:
					t.Fatalf("UNSPECIFIED value for review decision")
				}
			}

			require.Equal(t, test.approves, ac, "approves")
			require.Equal(t, test.trusts, tc, "trusts")
			require.Equal(t, test.blocks, bc, "blocks")

			if test.decision != nil {
				suite.validateLastFeedItem(t, pr, fmt.Sprintf(`{
				  "eventType": "REVIEW_DECISION",
				  "details": {
					"reviewDecision": {
					  "decision": "%s",
					  "userId": "%d"
					}
				  }
				}`, test.decision.String(), user.ID))
			} else {
				suite.validateLastFeedItem(t, pr, fmt.Sprintf(`{
				  "eventType": "REVIEW_DECISION",
				  "details": {
					"reviewDecision": {
					  "userId": "%d"
					}
				  }
				}`, user.ID))
			}
		})
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_WebSocket() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
		client := pb.NewPRReviewersServiceClient(suite.grpcClient)

		repo := suite.repos.Alpha
		pr := suite.makePullRequest(suite.users.Kopatych, nil)
		prID := grpc_marshalling.IDInverse(pr.ID)
		checkRevision := func(t *testing.T, rev string) {
			resp, err := client.List(ctx, &pb.ListReviewersRequest{
				PrId: prID,
			})
			require.NoError(t, err)
			require.Equal(t, rev, resp.Revision.Value)
		}

		err := suite.PullRequestService.WaitForMergeConflictCalculation(ctx, pr.ID)
		require.NoError(t, err)

		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
		suite.addRole(t, suite.users.Pikachu, repo, iam.Roles.RepositoriesDeveloper)

		t.Run("empty", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			checkRevision(t, "1")
			require.Empty(t, suite.WebSocketRequests())
		})

		t.Run("Add reviewers", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.Set(ctx, &pb.SetReviewersRequest{
				PrId: prID,
				UserIds: []string{
					grpc2.MarshalID(suite.users.Krosh.ID),
					grpc2.MarshalID(suite.users.Barash.ID),
				},
			})
			require.NoError(t, err)

			checkRevision(t, "2")
			suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrReviewersCollection)
		})

		t.Run("Add reviewers:no_change", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.Set(ctx, &pb.SetReviewersRequest{
				PrId: prID,
				UserIds: []string{
					grpc2.MarshalID(suite.users.Krosh.ID),
					grpc2.MarshalID(suite.users.Barash.ID),
				},
			})
			require.NoError(t, err)

			checkRevision(t, "2")
			require.Empty(t, suite.WebSocketRequests())
		})

		t.Run("Remove reviewers", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.Set(ctx, &pb.SetReviewersRequest{
				PrId:    prID,
				UserIds: []string{},
			})
			require.NoError(t, err)

			checkRevision(t, "3")
			suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrReviewersCollection)
		})

		t.Run("Remove reviewer:no_change", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.Set(ctx, &pb.SetReviewersRequest{
				PrId:    prID,
				UserIds: []string{},
			})
			require.NoError(t, err)

			checkRevision(t, "3")
			require.Empty(t, suite.WebSocketRequests())
		})

		t.Run("Remove with decision", func(t *testing.T) {
			tests := []struct {
				name         string
				decision     entities.PullRequestDecision
				wantRev      string
				wantMessages []entities.WsEntityType
			}{
				{
					name:     "Ship",
					decision: entities.PullRequestDecisions.Ship,
					wantRev:  "5",
					wantMessages: []entities.WsEntityType{
						entities.WsEntityTypes.PrReviewersCollection,
						entities.WsEntityTypes.PrShipsCollection,
					},
				},
				{
					name:     "StickyShip",
					decision: entities.PullRequestDecisions.StickyShip,
					wantRev:  "7",
					wantMessages: []entities.WsEntityType{
						entities.WsEntityTypes.PrReviewersCollection,
						entities.WsEntityTypes.PrShipsCollection,
					},
				},
				{
					name:     "Block",
					decision: entities.PullRequestDecisions.Block,
					wantRev:  "9",
					wantMessages: []entities.WsEntityType{
						entities.WsEntityTypes.PrReviewersCollection,
						entities.WsEntityTypes.PrBlocksCollection,
					},
				},
				{
					name:         "Abstain",
					decision:     entities.PullRequestDecisions.Abstain,
					wantRev:      "11",
					wantMessages: []entities.WsEntityType{entities.WsEntityTypes.PrReviewersCollection},
				},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					user := suite.users.Krosh

					// add
					_, err := client.Set(ctx, &pb.SetReviewersRequest{
						PrId:    prID,
						UserIds: []string{grpc2.MarshalID(user.ID)},
					})
					require.NoError(t, err)

					// make decision
					testutils.Expect(suite.client.As(testutils.UserIdentities.Pikachu).
						SetBody(&schemas.SetPullRequestDecision{
							Decision:          tt.decision,
							NotificationParam: testutils.SkipNotification,
						}).
						Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests/%d/decision", repo.FullSlug(), pr.ID))).
						MustBe(t, http.StatusNoContent)

					// remove
					require.NoError(t, suite.ClearWebSocketRequests())

					_, err = client.Set(ctx, &pb.SetReviewersRequest{
						PrId:    prID,
						UserIds: []string{},
					})
					require.NoError(t, err)

					checkRevision(t, tt.wantRev)
					suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), tt.wantMessages...)
				})
			}
		})

		t.Run("AutoAssign reviewer", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
			suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
			defer func() {
				require.NoError(t, cg.ClearDir())
			}()

			cg.Must(t, "checkout", "master")
			// empty old config to check newer is preferred
			commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
			commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), fmt.Sprintf(`
codereview:
  need_ships: 1
  rules:
    - patterns:
        - '**'
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
`, suite.users.Krosh.Username, suite.users.Barash.Username))
			cg.Must(t, "push")

			_, err := client.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId: prID,
			})
			require.NoError(t, err)

			checkRevision(t, "12")
			suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrReviewersCollection)
		})

		t.Run("AutoAssign reviewer:no_change", func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId: prID,
			})
			require.NoError(t, err)

			checkRevision(t, "12")
			require.Empty(t, suite.WebSocketRequests())
		})

		t.Run("UpdateReviewers: add 1, remove 1", func(t *testing.T) {
			_, err := client.Set(ctx, &pb.SetReviewersRequest{
				PrId: prID,
				UserIds: []string{
					grpc2.MarshalID(suite.users.Barash.ID),
				},
			})
			require.NoError(t, err)
			checkRevision(t, "13")

			require.NoError(t, suite.ClearWebSocketRequests())
			_, err = client.Update(ctx, &pb.UpdateReviewersRequest{
				PrId: prID,
				ReviewerDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(suite.users.Krosh.ID),
					},
					{
						Action: pb.DeltaAction_REMOVE,
						UserId: grpc2.MarshalID(suite.users.Barash.ID),
					},
				},
				Revision: utils.PtrFromValue("13"),
			})
			require.NoError(t, err)

			checkRevision(t, "14")
			suite.requireHasWsMessageTypes(t, fmt.Sprintf("repository_%d", repo.ID), entities.WsEntityTypes.PrReviewersCollection)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestPR_AutoAssignReviewers() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		pr := &schemas.PullRequest{}

		t.Run("init repo and pr", func(t *testing.T) {
			repo := suite.repos.TreeDiff
			author := suite.users.Kopatych
			suite.addRole(t, author, repo, iam.Roles.RepositoriesDeveloper)

			cg, tmpDir := suite.initCGit(author, repo.FullSlug())
			cg.Must(t, "checkout", "main")

			commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
			commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), fmt.Sprintf(`
codereview:
  need_ships: 1
  rules:
    - patterns:
        - '**'
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
    - patterns:
        - "**"
      reviewers:
        usernames:
          - "%s"
          - "%s"
        assign: 2
`, suite.users.Krosh.Username, suite.users.Barash.Username, suite.users.Barash.Username, suite.users.Pikachu.Username))
			cg.Must(t, "push")

			testutils.Expect(suite.client.As(suite.users.Kopatych.Identity).
				SetBody(&schemas.CreatePullRequestRequest{
					Title:             "PR with rules",
					Target:            "main",
					Source:            "branch",
					Publish:           true,
					NotificationParam: testutils.SkipNotification,
				}).
				SetResult(pr).
				Post(fmt.Sprintf("/api/v1/repos/%s/pullrequests", repo.FullSlug()))).MustBe(t, http.StatusCreated)
		})

		ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
		c := pb.NewPRReviewersServiceClient(suite.grpcClient)

		t.Run("empty reviewers", func(t *testing.T) {
			resp, err := c.List(ctx, &pb.ListReviewersRequest{
				PrId: string(pr.ID),
			})
			require.NoError(t, err)
			require.Len(t, resp.Reviewers, 0)
		})

		t.Run("AutoAssign", func(t *testing.T) {
			op, err := c.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId:                string(pr.ID),
				NotificationOptions: testutils.SkipNotificationPb,
			})
			require.NoError(t, err)

			deltas := testutils.UnmarshalGrpcResult[*pb.ReviewersOperationResult](t, op)
			slices.SortFunc(deltas.EffectiveDeltas, func(a, b *pb.ReviewerDelta) int {
				return strings.Compare(a.UserId, b.UserId)
			})

			yarequire.ProtoEqual(t, &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(suite.users.Krosh.ID),
					},
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(suite.users.Pikachu.ID),
					},
					{
						Action: pb.DeltaAction_ADD,
						UserId: grpc2.MarshalID(suite.users.Barash.ID),
					},
				},
				Revision: &pb.Revision{
					Value: "2",
					Count: utils.PtrFromValue(int32(3)),
				},
			}, deltas)
		})

		t.Run("now three reviewers", func(t *testing.T) {
			resp, err := c.List(ctx, &pb.ListReviewersRequest{
				PrId: string(pr.ID),
			})
			require.NoError(t, err)
			require.Len(t, resp.Reviewers, 3)
		})

		t.Run("nothing assign", func(t *testing.T) {
			currRev := "2"

			resp, err := c.List(ctx, &pb.ListReviewersRequest{
				PrId: string(pr.ID),
			})
			require.NoError(t, err)
			require.Len(t, resp.Reviewers, 3)
			require.Equal(t, currRev, resp.Revision.Value)

			op, err := c.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId:                string(pr.ID),
				NotificationOptions: testutils.SkipNotificationPb,
			})
			require.NoError(t, err)

			deltas := testutils.UnmarshalGrpcResult[*pb.ReviewersOperationResult](t, op)
			yarequire.ProtoEqual(t, &pb.ReviewersOperationResult{
				EffectiveDeltas: []*pb.ReviewerDelta{},
				Revision: &pb.Revision{
					Value: currRev,
					Count: utils.PtrFromValue(int32(3)),
				},
			}, deltas)
		})

		t.Run("nothing assign:wrong_revision", func(t *testing.T) {
			_, err := c.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId:                string(pr.ID),
				Revision:            utils.PtrFromValue("wrong"),
				NotificationOptions: testutils.SkipNotificationPb,
			})
			yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		})

		t.Run("Wrong revision", func(t *testing.T) {
			_, err := c.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
				PrId:                string(pr.ID),
				Revision:            utils.PtrFromValue("wrong"),
				NotificationOptions: testutils.SkipNotificationPb,
			})
			yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		})

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestPR_AutoAssignReviewers_InvalidYaml() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)

		repo := suite.repos.TreeDiff
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

		cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
		cg.Must(t, "checkout", "branch")

		commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), `
workflows:
  sample-workflow:
  tasks:
  - name: sample-task
  cubes:
`)
		cg.Must(t, "push")

		p := pb.NewPRServiceClient(suite.grpcClient)
		op, err := p.Create(ctx, &pb.CreatePullRequestRequest{
			RepoId: grpc2.MarshalID(repo.ID),
			Title:  "branch to main",
			Source: "branch",
			Target: "main",
		})
		require.NoError(t, err)
		pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
		require.NoError(t, err)

		c := pb.NewPRReviewersServiceClient(suite.grpcClient)

		op, err = c.AutoAssign(ctx, &pb.AutoAssignReviewersRequest{
			PrId:                pr.Id,
			NotificationOptions: testutils.SkipNotificationPb,
		})
		require.NoError(t, err)

		deltas := testutils.UnmarshalGrpcResult[*pb.ReviewersOperationResult](t, op)
		slices.SortFunc(deltas.EffectiveDeltas, func(a, b *pb.ReviewerDelta) int {
			return strings.Compare(a.UserId, b.UserId)
		})

		require.Len(t, deltas.EffectiveDeltas, 0)

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SuggestReviewers() {
	suite.RestoreOpensearch()

	t := suite.T()

	repoID, _, prID, _ := suite.prepareSuggestUsersTest()

	type testcase struct {
		name   string
		prID   uint64
		repoID uint64
		source string
		target string
		query  string
		user   *entities.User
	}

	testcases := []testcase{
		{
			name:   "new pr, query empty",
			repoID: repoID,
			source: "feature",
			target: "main",
			query:  "",
			user:   suite.users.Kopatych,
		},
		{
			name:   "new pr, query sa",
			repoID: repoID,
			source: "feature",
			target: "main",
			query:  consts.ServiceAccountPublicName,
			user:   suite.users.Kopatych,
		},
		{
			name:   "new pr, query k",
			repoID: repoID,
			source: "feature",
			target: "main",
			query:  "k",
			user:   suite.users.Kopatych,
		},
		{
			name:   "new pr, feature3",
			repoID: repoID,
			source: "feature3",
			target: "main",
			query:  "",
			user:   suite.users.Kopatych,
		},
		{
			name:   "new pr, feature4",
			repoID: repoID,
			source: "feature4",
			target: "main",
			query:  "",
			user:   suite.users.Kopatych,
		},
		{
			name:  "existing pr, query empty",
			prID:  prID,
			query: "",
			user:  suite.users.Kopatych,
		},
		{
			name:  "existing pr, query sa",
			prID:  prID,
			query: consts.ServiceAccountPublicName,
			user:  suite.users.Kopatych,
		},
		{
			name:  "existing pr, query k",
			prID:  prID,
			query: "k",
			user:  suite.users.Kopatych,
		},
		{
			name:  "existing pr, query bibi",
			prID:  prID,
			query: "bibi",
			user:  suite.users.Kopatych,
		},
		{
			name:  "not author can find himself",
			prID:  prID,
			query: "barash",
			user:  suite.users.Barash,
		},
	}

	c := pb.NewPRReviewersServiceClient(suite.grpcClient)

	test := func(tc testcase) func(t *testing.T) {
		return func(t *testing.T) {
			request := &pb.SuggestReviewersRequest{
				PrIdentity: &pb.SuggestReviewersRequest_NewPrIdentity{
					NewPrIdentity: &pb.SuggestReviewersRequest_NewPRIdentity{
						RepoId: grpc2.MarshalID(tc.repoID),
						Source: tc.source,
						Target: tc.target,
					},
				},
				Query: tc.query,
			}
			if tc.prID != 0 {
				request.PrIdentity = &pb.SuggestReviewersRequest_PrId{
					PrId: grpc2.MarshalID(tc.prID),
				}
			}
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := c.Suggest(ctx, request)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		}
	}

	for _, tc := range testcases {
		t.Run(tc.name, test(tc))
	}

	// Test fallback - when OS is unavailable
	suite.OpensearchBackendProxy.MakeUnavaliable()
	for _, tc := range testcases {
		t.Run(tc.name+" fallback", test(tc))
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SuggestReviewers_NoReviewRules() {
	suite.RestoreOpensearch()
	t := suite.T()

	repoID, _, _, _ := suite.prepareSuggestUsersTest()

	cg, tmpDir := suite.initCGit(suite.users.Kopatych, suite.repos.TreeDiff.FullSlug())
	cg.Must(t, "checkout", "main")
	commit(t, &cg, path.Join(tmpDir, suite.repos.TreeDiff.Slug, oyaml.ReviewPath), "")
	cg.Must(t, "push")

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	p := pb.NewPRServiceClient(suite.grpcClient)
	op, err := p.Create(ctx, &pb.CreatePullRequestRequest{
		RepoId:      grpc2.MarshalID(repoID),
		Title:       "feature2 to main-without-rules",
		Source:      "feature2",
		Target:      "main-without-rules",
		ReviewerIds: []string{grpc2.MarshalID(suite.users.Slowpoke.ID)},
		Publish:     true,
	})
	require.NoError(t, err)
	pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
	require.NoError(t, err)
	prID, err := grpc_marshalling.IDDirect(pr.Id)
	require.NoError(t, err)

	c := pb.NewPRReviewersServiceClient(suite.grpcClient)

	testcases := []struct {
		name   string
		prID   uint64
		repoID uint64
		source string
		target string
		query  string
	}{
		{
			name:   "new pr, query empty",
			repoID: repoID,
			source: "feature",
			target: "main-without-rules",
			query:  "",
		},
		{
			name:   "new pr, query k",
			repoID: repoID,
			source: "feature",
			target: "main-without-rules",
			query:  "k",
		},
		{
			name:  "existing pr, query empty",
			prID:  prID,
			query: "",
		},
		{
			name:  "existing pr, query k",
			prID:  prID,
			query: "k",
		},
		{
			name:  "existing pr, query bibi",
			prID:  prID,
			query: "bibi",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			request := &pb.SuggestReviewersRequest{
				PrIdentity: &pb.SuggestReviewersRequest_NewPrIdentity{
					NewPrIdentity: &pb.SuggestReviewersRequest_NewPRIdentity{
						RepoId: grpc2.MarshalID(tc.repoID),
						Source: tc.source,
						Target: tc.target,
					},
				},
				Query: tc.query,
			}
			if tc.prID != 0 {
				request.PrIdentity = &pb.SuggestReviewersRequest_PrId{
					PrId: grpc2.MarshalID(tc.prID),
				}
			}
			resp, err := c.Suggest(ctx, request)
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
		})
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SuggestReviewers_Limits() {
	suite.RestoreOpensearch()

	t := suite.T()

	repo := suite.repos.TreeDiff

	// create many users
	users := make([]*entities.User, 0)
	for i := 1; i <= 20; i++ {
		ctx := context.Background()

		identity := entities.UserIdentity{
			ID:  fmt.Sprintf("user-%c", 'a'+i),
			Src: entities.IdentityProviders.IAM,
		}
		suite.usersPk++
		user, err := suite.UserService.CreateUser(ctx, testutils.NewStubAuthenticator(&identity), interfaces.UserCreateArgs{
			ID:       suite.usersPk,
			Identity: identity,
		}, false)
		require.NoError(t, err)

		_, err = suite.UserService.SetFlag(ctx, user, entities.UserFlags.Onboarded, true)
		require.NoError(t, err)

		users = append(users, user)
	}

	// add users to org, expecting org members: Admin, users[:10]
	for _, user := range users[:10] {
		err := suite.MembershipRepo.Create(context.Background(), user.Identity, suite.orgs.Yandex.Identity)
		require.NoError(t, err)
	}

	// add repo contributors, expecting contributors: Admin, Kopatych, users[10:20]
	for _, user := range users[10:20] {
		suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
		cg, tmpDir := suite.initCGit(user, repo.FullSlug())
		cg.Must(t, "checkout", "main")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, user.Username), "content")
		cg.Must(t, "push")
	}

	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)
	// create new branch with changes in dir1 and dir3
	cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
	cg.Must(t, "checkout", "-b", "feature")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "new.txt"), "content")
	commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir3", "new.txt"), "content")
	cg.Must(t, "push", "--set-upstream", "origin", "feature")

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	suite.OpensearchKit.RefreshIndex(context.Background())

	testcases := []struct {
		name   string
		prID   uint64
		repoID uint64
		source string
		target string
		query  string
	}{
		{
			name:   "new pr, query empty",
			repoID: repo.ID,
			source: "feature",
			target: "main",
			query:  "",
		},
		{
			name:   "new pr, query k",
			repoID: repo.ID,
			source: "feature",
			target: "main",
			query:  "k",
		},
	}

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewPRReviewersServiceClient(suite.grpcClient)

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			request := &pb.SuggestReviewersRequest{
				PrIdentity: &pb.SuggestReviewersRequest_NewPrIdentity{
					NewPrIdentity: &pb.SuggestReviewersRequest_NewPRIdentity{
						RepoId: grpc2.MarshalID(tc.repoID),
						Source: tc.source,
						Target: tc.target,
					},
				},
				Query: tc.query,
			}
			if tc.prID != 0 {
				request.PrIdentity = &pb.SuggestReviewersRequest_PrId{
					PrId: grpc2.MarshalID(tc.prID),
				}
			}
			resp, err := c.Suggest(ctx, request)
			require.NoError(t, err)
			//yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "id", "personal_org_id", "uuid"))
		})
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SuggestReviewers_MultiRepoContributor() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		suite.RestoreOpensearch()

		t := suite.T()

		repo := suite.repos.TreeDiff

		// add repo contributors, expecting contributors: Admin, Kopatych, Barash
		for _, user := range []*entities.User{suite.users.Barash, suite.users.Kopatych} {
			suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
			cg, tmpDir := suite.initCGit(user, repo.FullSlug())
			cg.Must(t, "checkout", "main")
			commit(t, &cg, path.Join(tmpDir, repo.Slug, user.Username), "content")
			cg.Must(t, "push")
		}

		// Barash contributes to another repo
		suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)
		cg, tmpDir := suite.initCGit(suite.users.Barash, suite.repos.Alpha.FullSlug())
		cg.Must(t, "checkout", "master")
		cg.Must(t, "add", ".")
		commit(t, &cg, path.Join(tmpDir, suite.repos.Alpha.Slug, suite.users.Barash.Username), "content")
		cg.Must(t, "push")

		// add codereview rules
		cg, tmpDir = suite.initCGit(suite.users.Kopatych, repo.FullSlug())
		cg.Must(t, "checkout", "main")

		commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), fmt.Sprintf(`
codereview:
  auto_assign: false
  need_ships: 1
  rules:
    - patterns:
        - "dir1/**"
      reviewers:
        usernames:
          - "%s"
        assign: 1
`, suite.users.Barash.Username))
		cg.Must(t, "push")

		// create new branch with changes in dir1
		cg, tmpDir = suite.initCGit(suite.users.Kopatych, repo.FullSlug())
		cg.Must(t, "checkout", "-b", "feature")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "new.txt"), "content")
		cg.Must(t, "push", "--set-upstream", "origin", "feature")

		ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
		c := pb.NewPRReviewersServiceClient(suite.grpcClient)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		suite.OpensearchKit.RefreshIndex(ctx)

		type testcase struct {
			name   string
			prID   uint64
			repoID uint64
			source string
			target string
			query  string
		}

		testcases := []testcase{
			{
				name:   "new pr, query empty",
				repoID: repo.ID,
				source: "feature",
				target: "main",
				query:  "",
			},
		}

		test := func(tc testcase) func(t *testing.T) {
			return func(t *testing.T) {
				request := &pb.SuggestReviewersRequest{
					PrIdentity: &pb.SuggestReviewersRequest_NewPrIdentity{
						NewPrIdentity: &pb.SuggestReviewersRequest_NewPRIdentity{
							RepoId: grpc2.MarshalID(tc.repoID),
							Source: tc.source,
							Target: tc.target,
						},
					},
					Query: tc.query,
				}
				if tc.prID != 0 {
					request.PrIdentity = &pb.SuggestReviewersRequest_PrId{
						PrId: grpc2.MarshalID(tc.prID),
					}
				}
				resp, err := c.Suggest(ctx, request)
				require.NoError(t, err)
				//yarequire.ProtoDumpFixture(t, resp)
				yarequire.ProtoCompareWithFixture(t, resp,
					protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "uuid"))
			}
		}

		for _, tc := range testcases {
			t.Run(tc.name, test(tc))
		}

		// Test fallback - when OS is unavailable
		suite.OpensearchBackendProxy.MakeUnavaliable()
		for _, tc := range testcases {
			t.Run(tc.name+" fallback", test(tc))
		}

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestReviewersHandler_SuggestReviewers_InvalidYaml() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		suite.RestoreOpensearch()

		t := suite.T()

		repo := suite.repos.TreeDiff

		// add invalid yaml
		suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesDeveloper)

		cg, tmpDir := suite.initCGit(suite.users.Kopatych, repo.FullSlug())
		cg.Must(t, "checkout", "main")

		commit(t, &cg, path.Join(tmpDir, repo.Slug, oyaml.OldPath), "")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, configPath), `
workflows:
  sample-workflow:
  tasks:
  - name: sample-task
  cubes:

codereview:
  auto_assign: false
  need_ships: 1
`)
		cg.Must(t, "push")

		// create new branch
		cg, tmpDir = suite.initCGit(suite.users.Kopatych, repo.FullSlug())
		cg.Must(t, "checkout", "-b", "feature")
		commit(t, &cg, path.Join(tmpDir, repo.Slug, "dir1", "new.txt"), "content")
		cg.Must(t, "push", "--set-upstream", "origin", "feature")

		ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
		c := pb.NewPRReviewersServiceClient(suite.grpcClient)

		p := pb.NewPRServiceClient(suite.grpcClient)
		op, err := p.Create(ctx, &pb.CreatePullRequestRequest{
			RepoId:      grpc2.MarshalID(repo.ID),
			Title:       "feature to main",
			Source:      "feature",
			Target:      "main",
			ReviewerIds: []string{grpc2.MarshalID(suite.users.Krosh.ID)},
		})
		require.NoError(t, err)
		pr, err := grpc_marshalling.OperationResponse(op, &pb.PullRequest{})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
		suite.OpensearchKit.RefreshIndex(ctx)

		testcases := []struct {
			name   string
			prID   uint64
			repoID uint64
			source string
			target string
			query  string
		}{
			{
				name:   "new pr, query empty",
				repoID: repo.ID,
				source: "feature",
				target: "main",
				query:  "k",
			},
			{
				name:  "existing pr, query empty",
				prID:  grpc2.StrToUInt64(pr.Id),
				query: "k",
			},
		}

		for _, tc := range testcases {
			t.Run(tc.name, func(t *testing.T) {
				request := &pb.SuggestReviewersRequest{
					PrIdentity: &pb.SuggestReviewersRequest_NewPrIdentity{
						NewPrIdentity: &pb.SuggestReviewersRequest_NewPRIdentity{
							RepoId: grpc2.MarshalID(tc.repoID),
							Source: tc.source,
							Target: tc.target,
						},
					},
					Query: tc.query,
				}
				if tc.prID != 0 {
					request.PrIdentity = &pb.SuggestReviewersRequest_PrId{
						PrId: grpc2.MarshalID(tc.prID),
					}
				}
				_, err := c.Suggest(ctx, request)
				require.NoError(t, err)
			})
		}

		suite.AfterTest("", "")
	}
}

func (suite *RwApiTestSuite) TestPullRequestReviewers_WebSocket_Ships() {
	t := suite.T()

	repo := suite.repos.Alpha
	pr := suite.makePullRequest(suite.users.Kopatych, nil)
	for _, user := range []*entities.User{suite.users.Krosh, suite.users.Barash, suite.users.Kopatych, suite.users.PinPublic} {
		suite.addReviewer(t, pr, user)
	}

	client := pb.NewPRReviewersServiceClient(suite.grpcClient)
	_, err := client.SetDecision(testutils.AuthorizeGRPC(suite.users.PinPublic.Identity), &pb.SetDecisionRequest{
		PrId:           grpc_marshalling.IDInverse(pr.ID),
		ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		collection string
		decision   *pb.ReviewDecision
		user       *entities.User
	}{
		{
			name:       "approve",
			collection: "PrShipsCollection",
			decision:   utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
			user:       suite.users.Krosh,
		},
		{
			name:       "trust",
			collection: "PrShipsCollection",
			decision:   utils.PtrFromValue(pb.ReviewDecision_RD_TRUST),
			user:       suite.users.Barash,
		},
		{
			name:       "block",
			collection: "PrBlocksCollection",
			decision:   utils.PtrFromValue(pb.ReviewDecision_RD_BLOCK),
			user:       suite.users.Kopatych,
		},
		{
			name:       "remove approval",
			collection: "PrShipsCollection",
			decision:   nil,
			user:       suite.users.PinPublic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, suite.ClearWebSocketRequests())

			_, err := client.SetDecision(testutils.AuthorizeGRPC(tt.user.Identity), &pb.SetDecisionRequest{
				PrId:           grpc_marshalling.IDInverse(pr.ID),
				ReviewDecision: tt.decision,
			})
			require.NoError(t, err)

			require.NotEmpty(t, suite.WebSocketRequests())
			suite.requireHasWsMessage(t, fmt.Sprintf("repository_%d", repo.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"repoID": "%d",
					"prID": "%d"
				},
				"initiatorID": "%d"
			}`, tt.collection, repo.ID, pr.ID, tt.user.ID))
		})
	}
}

func (suite *RwApiTestSuite) TestPullRequestReviewers_WebSocket_Me() {
	t := suite.T()
	ctx := context.Background()

	var pr *entities.PullRequest
	author := suite.users.Kopatych
	t.Run("Create PR", func(t *testing.T) {
		pr = suite.makePullRequest(author, &makePrOptions{
			Publish: utils.PtrFromValue(false),
		})
		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", author.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"userID": "%d"
				},
				"initiatorID": "%d"
			}`,
			entities.WsEntityTypes.MyAuthoredPRsCollection, author.ID, author.ID),
		)
	})

	t.Run("Publish PR", func(t *testing.T) {
		require.NoError(t, suite.ClearWebSocketRequests())
		err := suite.PullRequestService.Publish(ctx, pr, suite.orgs.Yandex.ID, author, suite.getFakeAuthenticator(author.Identity), entities.NotifyOptions{})
		require.NoError(t, err)
		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", author.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"prID": "%d",
					"repoID": "%d",
					"userID": "%d"
				},
				"initiatorID": "%d"
			}`,
			entities.WsEntityTypes.PullRequest, pr.ID, pr.RepoID, author.ID, author.ID),
		)
	})

	reviewer := suite.users.Krosh
	t.Run("Assign", func(t *testing.T) {
		require.NoError(t, suite.ClearWebSocketRequests())

		for _, user := range []*entities.User{suite.users.Krosh, suite.users.Barash} {
			suite.addReviewer(t, pr, user)
		}

		user := suite.users.Krosh
		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", user.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"userID": "%d"
				},
				"initiatorID": "%d"
			}`,
			entities.WsEntityTypes.MyReviewedPRsCollection, user.ID, author.ID),
		)

		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", user.ID), fmt.Sprintf(`{
			"identity": {
				"type": "%s",
				"userID": "%d"
			},
			"initiatorID": "%d"
		}`,
			entities.WsEntityTypes.MyAllPRsCollection, user.ID, author.ID),
		)
	})

	t.Run("Update PR", func(t *testing.T) {
		require.NoError(t, suite.ClearWebSocketRequests())
		err := suite.PullRequestService.Update(ctx, pr, author.ID, suite.getFakeAuthenticator(author.Identity), &entities.PullRequestUpdateFields{
			Title: utils.PtrFromValue("New title"),
		}, entities.NotifyOptions{}, signals.SignalFlowTypes.Default)
		require.NoError(t, err)
		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", reviewer.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"prID": "%d",
					"repoID": "%d",
					"userID": "%d"
				},
				"initiatorID": "%d"
			}`,
			entities.WsEntityTypes.PullRequest, pr.ID, pr.RepoID, reviewer.ID, author.ID),
		)
	})

	t.Run("Remove", func(t *testing.T) {
		for _, user := range []*entities.User{suite.users.Krosh, suite.users.Barash} {
			suite.addReviewer(t, pr, user)
		}

		require.NoError(t, suite.ClearWebSocketRequests())
		err := suite.PullRequestService.DeleteReviewer(context.Background(), pr, author, suite.users.Barash, entities.NotifyOptions{})
		require.NoError(t, err)

		user := suite.users.Barash
		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", user.ID), fmt.Sprintf(`{
				"identity": {
					"type": "%s",
					"userID": "%d"
				},
				"initiatorID": "%d"
			}`,
			entities.WsEntityTypes.MyReviewedPRsCollection, user.ID, author.ID),
		)

		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", user.ID), fmt.Sprintf(`{
			"identity": {
				"type": "%s",
				"userID": "%d"
			},
			"initiatorID": "%d"
		}`,
			entities.WsEntityTypes.MyAllPRsCollection, user.ID, author.ID),
		)
	})
}

func (suite *RwApiTestSuite) TestSetDecision_StickyShipDisabled() {
	t := suite.T()

	suite.mustBash(suite.repos.Alpha, `
		set -e
		git branch -m master

		# Create config with disable_trust: true
		mkdir -p .sourcecraft
		cat > .sourcecraft/review.yaml << 'EOF'
codereview:
  disable_trust: true
EOF
		git add .
		git commit -m "add config"

		# Create a feature branch with changes
		git checkout -b feature
		echo "test content" > feature.txt
		git add .
		git commit -m "add feature"
	`)

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Source:  "feature",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewPRReviewersServiceClient(suite.grpcClient)

	t.Run("failed trust", func(t *testing.T) {
		_, err := client.SetDecision(ctx, &pb.SetDecisionRequest{
			PrId:           grpc_marshalling.IDInverse(pr.ID),
			ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_TRUST),
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("success approve", func(t *testing.T) {
		_, err := client.SetDecision(ctx, &pb.SetDecisionRequest{
			PrId:           grpc_marshalling.IDInverse(pr.ID),
			ReviewDecision: utils.PtrFromValue(pb.ReviewDecision_RD_APPROVE),
		})
		require.NoError(t, err)
	})
}
