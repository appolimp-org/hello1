package integrationtests

import (
	"common/grpc"
	"gitcore/internal/access/stubs"
	except "gitcore/internal/exceptions"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"

	"common/testutils/yarequire"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	sdkop "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/operation"
	operation "bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/generated/mocks"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *IntegrationTestSuite) prepareSDKResponse(resp proto.Message) sdkop.Operation {
	t := suite.T()

	empty, err := anypb.New(&emptypb.Empty{})
	require.NoError(t, err)

	resultResponse, err := anypb.New(resp)
	require.NoError(t, err)

	opX, err := sdkop.New(
		&operation.Operation{
			Id:       "deadbeef",
			Done:     true,
			Metadata: empty,
			Result: &operation.Operation_Response{
				Response: resultResponse,
			},
		},
		&sdkop.Concretization{
			MetadataType: &emptypb.Empty{},
			ResponseType: resp,
			GetResourceID: func(metadata proto.Message) string {
				return "resourceID"
			},
		},
	)
	require.NoError(t, err)
	return *opX
}

func (suite *RwApiTestSuite) TestCreateInviteGRPC() {
	t := suite.T()

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	o := &organizationmanagersdk.InvitationCreateOperation{
		Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
			ValidInvitations: []*organizationmanager.Invitation{
				{
					Id:     "foobar",
					Status: organizationmanager.Invitation_PENDING,
					Identity: &organizationmanager.Invitation_Invitee_{
						Invitee: &organizationmanager.Invitation_Invitee{Email: "hello@world.com"},
					},
				},
			},
		}),
	}
	cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(o, nil)

	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	email := "hello@world.com"

	req := &pb.CreateInviteRequest{
		OrgId:     orgID,
		TtlInDays: nil,
		Invitee:   &pb.CreateInviteRequest_Email{Email: email},
	}

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)

	t.Run("permission denied", func(t *testing.T) {
		_, err := client.Create(ctx, req)
		require.Error(t, err)
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("happy path", func(t *testing.T) {
		suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

		op, err := client.Create(ctx, req)
		require.NoError(t, err)

		var createOp *pb.CreateInviteOperation

		require.Eventuallyf(t, func() bool {
			createOp, err = opClient.GetCreateInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
			require.NoError(t, err)
			return createOp.Done
		}, time.Second*20, 50*time.Millisecond, "operation not finished")

		resp, ok := createOp.Result.(*pb.CreateInviteOperation_Response)
		require.True(t, ok)

		require.Equal(t, email, resp.Response.InviteeEmail)
	})
}

func (suite *RwApiTestSuite) TestCreateInviteGRPCAlreadySent() {
	t := suite.T()
	email := "hello@world.com"
	user := suite.users.Krosh

	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.Admin},
	}))

	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	for _, tc := range []struct {
		name         string
		prepare      func(cloudMock *mocks.MockYCPSDKInvitationClient)
		request      *pb.CreateInviteRequest
		expectErrMsg *string
	}{
		{
			name: "non-expired pending",
			prepare: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				gomock.InOrder(
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil, status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")).AnyTimes(),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{
							Invitations: []*organizationmanager.Invitation{
								{
									Id: "something",
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{
											Email: email,
										},
									},
									Status:   organizationmanager.Invitation_PENDING,
									NotAfter: timestamppb.New(time.Now().UTC().Add(24 * time.Hour)),
								},
							},
							NextPageToken: "",
						}, nil),
				)
			},
			request: &pb.CreateInviteRequest{
				OrgId:     orgID,
				TtlInDays: nil,
				Invitee:   &pb.CreateInviteRequest_Email{Email: email},
			},
			expectErrMsg: &except.InviteAlreadyCreated.Template,
		},
		{
			name: "expired pending",
			prepare: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				do := &organizationmanagersdk.InvitationDeleteOperation{Operation: suite.prepareSDKResponse(&emptypb.Empty{})}
				co := &organizationmanagersdk.InvitationCreateOperation{Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
					ValidInvitations: []*organizationmanager.Invitation{{Id: "foobar"}},
				})}

				gomock.InOrder(
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil, status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")).AnyTimes(),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{
							Invitations: []*organizationmanager.Invitation{
								{
									Id: "something",
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{
											Email: email,
										},
									},
									Status:   organizationmanager.Invitation_PENDING,
									NotAfter: timestamppb.New(time.Now().UTC().Add(-1 * time.Minute)),
								},
							},
							NextPageToken: "",
						}, nil),
					cloudMock.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(do, nil),
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(co, nil),
				)
			},
			request: &pb.CreateInviteRequest{
				OrgId:     orgID,
				TtlInDays: nil,
				Invitee:   &pb.CreateInviteRequest_Email{Email: email},
			},
		},
		{
			name: "broken creating",
			prepare: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				do := &organizationmanagersdk.InvitationDeleteOperation{Operation: suite.prepareSDKResponse(&emptypb.Empty{})}
				co := &organizationmanagersdk.InvitationCreateOperation{Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
					ValidInvitations: []*organizationmanager.Invitation{{Id: "foobar"}},
				})}

				gomock.InOrder(
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil, status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")).AnyTimes(),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{}, nil),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{
							Invitations: []*organizationmanager.Invitation{
								{
									Id: "something",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
									Status:    organizationmanager.Invitation_CREATING,
									CreatedAt: timestamppb.New(time.Now().UTC().Add(-11 * time.Minute)),
									NotAfter:  timestamppb.New(time.Now().UTC().Add(24 * time.Hour)),
								},
							},
							NextPageToken: "",
						}, nil),
					cloudMock.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(do, nil),
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(co, nil),
				)
			},
			request: &pb.CreateInviteRequest{
				OrgId:     orgID,
				TtlInDays: nil,
				Invitee:   &pb.CreateInviteRequest_UserId{UserId: grpc.MarshalID(user.ID)},
			},
		},
		{
			name: "creating now",
			prepare: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				gomock.InOrder(
					cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil, status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")).AnyTimes(),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{}, nil),
					cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(&organizationmanager.ListOrganizationInvitationsResponse{
							Invitations: []*organizationmanager.Invitation{
								{
									Id: "something",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
									Status:    organizationmanager.Invitation_CREATING,
									CreatedAt: timestamppb.New(time.Now().UTC()),
									NotAfter:  timestamppb.New(time.Now().UTC().Add(24 * time.Hour)),
								},
							},
							NextPageToken: "",
						}, nil),
				)
			},
			request: &pb.CreateInviteRequest{
				OrgId:     orgID,
				TtlInDays: nil,
				Invitee:   &pb.CreateInviteRequest_UserId{UserId: grpc.MarshalID(user.ID)},
			},
			expectErrMsg: &except.InviteAlreadyCreated.Template,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, reporter := testutils.NewMockController(t)
			defer reporter.Finish(ctrl)

			cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
			tc.prepare(cloudMock)
			suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

			op, err := client.Create(ctx, tc.request)
			require.NoError(t, err)

			var createOp *pb.CreateInviteOperation

			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetCreateInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			if tc.expectErrMsg != nil {
				resp, ok := createOp.Result.(*pb.CreateInviteOperation_Error)
				require.True(t, ok)
				require.Equal(t, *tc.expectErrMsg, resp.Error.Message)
			} else {
				_, ok := createOp.Result.(*pb.CreateInviteOperation_Response)
				require.True(t, ok)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestCreateInviteGRPC_byUserID() {
	t := suite.T()
	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	invitee := suite.users.Krosh
	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	o := &organizationmanagersdk.InvitationCreateOperation{
		Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
			ValidInvitations: []*organizationmanager.Invitation{
				{
					Id:     "foobar",
					Status: organizationmanager.Invitation_PENDING,
					Identity: &organizationmanager.Invitation_UserAccount_{
						UserAccount: &organizationmanager.Invitation_UserAccount{Id: invitee.Identity.ID},
					},
					ServiceUri: org.Slug,
				},
			},
		}),
	}
	cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(o, nil)

	req := &pb.CreateInviteRequest{
		OrgId:     orgID,
		TtlInDays: nil,
		Invitee:   &pb.CreateInviteRequest_UserId{UserId: grpc_marshalling.IDInverse(invitee.ID)},
	}

	t.Run("permission denied", func(t *testing.T) {
		_, err := client.Create(ctx, req)
		require.Error(t, err)
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("happy path", func(t *testing.T) {
		suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

		op, err := client.Create(ctx, req)
		require.NoError(t, err)

		var createOp *pb.CreateInviteOperation
		require.Eventuallyf(t, func() bool {
			createOp, err = opClient.GetCreateInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
			require.NoError(t, err)
			return createOp.Done
		}, time.Second*20, 50*time.Millisecond, "operation not finished")

		resp, ok := createOp.Result.(*pb.CreateInviteOperation_Response)
		require.True(t, ok)

		require.Equal(t, "", resp.Response.InviteeEmail)
		require.Equal(t, grpc_marshalling.IDInverse(invitee.ID), resp.Response.InviteeId)
	})

	t.Run("already a member", func(t *testing.T) {
		suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

		user, err := suite.UserService.CreateUser(ctx, access.NullAuthenticator, interfaces.UserCreateArgs{
			Identity: stubs.StubbedIAMOrgsMember(),
		}, false)
		require.NoError(t, err)
		req.Invitee = &pb.CreateInviteRequest_UserId{UserId: grpc_marshalling.IDInverse(user.ID)}

		_, err = client.Create(ctx, req)
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})
}

func (suite *RwApiTestSuite) TestCreateInviteGRPC_byEmail() {
	t := suite.T()
	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	email := "sovunya@ya.ru"

	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	o := &organizationmanagersdk.InvitationCreateOperation{
		Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
			ValidInvitations: []*organizationmanager.Invitation{
				{
					Id:     "foobar",
					Status: organizationmanager.Invitation_PENDING,
					Identity: &organizationmanager.Invitation_Invitee_{
						Invitee: &organizationmanager.Invitation_Invitee{
							Email: email,
						},
					},
					ServiceUri: org.Slug,
				},
			},
		}),
	}
	cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(o, nil).
		AnyTimes()

	req := &pb.CreateInviteRequest{
		OrgId:     orgID,
		TtlInDays: nil,
		Invitee:   &pb.CreateInviteRequest_Email{Email: email},
	}

	t.Run("permission denied", func(t *testing.T) {
		_, err := client.Create(ctx, req)
		require.Error(t, err)
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("happy path", func(t *testing.T) {
		suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

		op, err := client.Create(ctx, req)
		require.NoError(t, err)

		var createOp *pb.CreateInviteOperation
		require.Eventuallyf(t, func() bool {
			createOp, err = opClient.GetCreateInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
			require.NoError(t, err)
			return createOp.Done
		}, time.Second*20, 50*time.Millisecond, "operation not finished")

		resp, ok := createOp.Result.(*pb.CreateInviteOperation_Response)
		require.True(t, ok)

		require.Equal(t, email, resp.Response.InviteeEmail)
		require.Equal(t, "0", resp.Response.InviteeId)
	})

	t.Run("already registered", func(t *testing.T) {
		suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

		userIdentity := entities.UserIdentity{
			ID:  "sovunya",
			Src: entities.IdentityProviders.IAM,
		}
		_, err := pb.NewMeServiceClient(suite.grpcClient).GetProfile(
			testutils.AuthorizeGRPC(userIdentity),
			&pb.GetMyProfileRequest{},
		)
		require.NoError(t, err)
		suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
			PublicName:        "Sovunya",
			PreferredUsername: "sovunya",
			Email:             "sovunya@ya.ru",
		})

		_, err = client.Create(ctx, req)
		require.NoError(t, err)
		//yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})
}
