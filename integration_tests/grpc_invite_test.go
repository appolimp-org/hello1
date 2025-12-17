package integrationtests

import (
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	"gitcore/internal/generated/mocks"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestAcceptInviteGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)

	cases := []struct {
		name      string
		user      *entities.User
		request   *pb.AcceptInviteRequest
		setupMock func(cloudMock *mocks.MockYCPSDKInvitationClient)
		wantCode  codes.Code
	}{
		{
			name: "invite_id",
			user: suite.users.Admin,
			request: &pb.AcceptInviteRequest{
				InviteId: "invite_id",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().Accept(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationAcceptOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.AcceptInvitationResponse{}),
					}, nil)
			},
		},
		{
			name: "invite_code",
			user: suite.users.Admin,
			request: &pb.AcceptInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().Accept(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationAcceptOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.AcceptInvitationResponse{}),
					}, nil)
			},
		},
		{
			name: "expired",
			user: suite.users.Admin,
			request: &pb.AcceptInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.FailedPrecondition, "Invitation 'asd' has been expired after 123.")
				cloudMock.EXPECT().Accept(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "mismatch",
			user: suite.users.Admin,
			request: &pb.AcceptInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.FailedPrecondition, "Current subject cannot accept or reject invitation 'asd'")
				cloudMock.EXPECT().Accept(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "fatal",
			user: suite.users.Admin,
			request: &pb.AcceptInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.Internal, "internal error")
				cloudMock.EXPECT().Accept(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "anonymous",
			user: entities.NewAnonymousUser(),
			request: &pb.AcceptInviteRequest{
				InviteCode: "code",
			},
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "empty_request",
			user:     suite.users.Kopatych,
			request:  &pb.AcceptInviteRequest{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, reporter := testutils.NewMockController(t)
			defer reporter.Finish(ctrl)

			if tc.setupMock != nil {
				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			op, err := client.Accept(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.wantCode, err)
			if tc.wantCode != codes.OK {
				return
			}

			require.Eventuallyf(t, func() bool {
				op, err = opClient.Get(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return op.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			// yarequire.ProtoDumpFixture(t, op)
			yarequire.ProtoCompareWithFixture(t, op,
				protocmp.IgnoreFields(&operation.Operation{}, "id", "created_at", "modified_at"),
			)
		})
	}
}

func (suite *RepoApiTestSuite) TestRejectInviteGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)

	cases := []struct {
		name      string
		user      *entities.User
		request   *pb.RejectInviteRequest
		setupMock func(cloudMock *mocks.MockYCPSDKInvitationClient)
		wantCode  codes.Code
	}{
		{
			name: "invite_id",
			user: suite.users.Admin,
			request: &pb.RejectInviteRequest{
				InviteId: "invite_id",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().Reject(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationRejectOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.RejectInvitationResponse{}),
					}, nil)
			},
		},
		{
			name: "invite_code",
			user: suite.users.Admin,
			request: &pb.RejectInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().Reject(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationRejectOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.RejectInvitationResponse{}),
					}, nil)
			},
		},
		{
			name: "expired",
			user: suite.users.Admin,
			request: &pb.RejectInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.FailedPrecondition, "Invitation 'asd' has been expired after 123.")
				cloudMock.EXPECT().Reject(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "mismatch",
			user: suite.users.Admin,
			request: &pb.RejectInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.FailedPrecondition, "Current subject cannot accept or reject invitation 'asd'")
				cloudMock.EXPECT().Reject(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "fatal",
			user: suite.users.Admin,
			request: &pb.RejectInviteRequest{
				InviteCode: "invite_code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.Internal, "internal error")
				cloudMock.EXPECT().Reject(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr)
			},
		},
		{
			name: "anonymous",
			user: entities.NewAnonymousUser(),
			request: &pb.RejectInviteRequest{
				InviteCode: "code",
			},
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "empty_request",
			user:     suite.users.Kopatych,
			request:  &pb.RejectInviteRequest{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, reporter := testutils.NewMockController(t)
			defer reporter.Finish(ctrl)

			if tc.setupMock != nil {
				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			op, err := client.Reject(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.wantCode, err)
			if tc.wantCode != codes.OK {
				return
			}

			require.Eventuallyf(t, func() bool {
				op, err = opClient.Get(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return op.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			// yarequire.ProtoDumpFixture(t, op)
			yarequire.ProtoCompareWithFixture(t, op,
				protocmp.IgnoreFields(&operation.Operation{}, "id", "created_at", "modified_at"),
			)
		})
	}
}
