package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/generated/mocks"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"google.golang.org/grpc"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGrpcInvitesCreateBulk_ByEmail() {
	t := suite.T()

	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	for _, tc := range []struct {
		name              string
		emails            []string
		response          *organizationmanager.CreateInvitationsResponse
		grpcError         error
		expectValidEmails []string
		expectInvalid     map[string]string
	}{
		{
			name:   "ok",
			emails: []string{"hello1@world.com", "hello2@world.com"},
			response: &organizationmanager.CreateInvitationsResponse{
				ValidInvitations: []*organizationmanager.Invitation{
					{
						Status: organizationmanager.Invitation_PENDING,
						Identity: &organizationmanager.Invitation_Invitee_{
							Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
						},
					},
					{
						Status: organizationmanager.Invitation_CREATING,
						Identity: &organizationmanager.Invitation_Invitee_{
							Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
						},
					},
				},
			},
			expectValidEmails: []string{"hello1@world.com", "hello2@world.com"},
			expectInvalid:     map[string]string{},
		},
		{
			name:   "with errors",
			emails: []string{"hello1@world.com", "hello2@world.com"},
			response: &organizationmanager.CreateInvitationsResponse{
				ValidInvitations: []*organizationmanager.Invitation{
					{
						Status: organizationmanager.Invitation_PENDING,
						Identity: &organizationmanager.Invitation_Invitee_{
							Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
						},
					},
				},
				InvalidInvitations: []*organizationmanager.Invitation{
					{
						Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
						Identity: &organizationmanager.Invitation_Invitee_{
							Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
						},
					},
				},
			},
			expectValidEmails: []string{"hello1@world.com"},
			expectInvalid: map[string]string{
				"hello2@world.com": "IAM rejected creation of the invite",
			},
		},
		{
			name:              "all invalid",
			emails:            []string{"hello1@world.com", "hello2@world.com"},
			grpcError:         status.New(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.").Err(),
			expectValidEmails: []string{},
			expectInvalid: map[string]string{
				"hello1@world.com": except.InviteAlreadyCreated.Template,
				"hello2@world.com": except.InviteAlreadyCreated.Template,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, reporter := testutils.NewMockController(t)
			defer reporter.Finish(ctrl)

			cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
			suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

			cloudMock.EXPECT().
				Create(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
					if tc.grpcError != nil {
						require.Len(t, in.Invitations, len(tc.expectInvalid))
						return nil, tc.grpcError
					}

					require.Len(t, in.Invitations, len(tc.response.ValidInvitations)+len(tc.response.InvalidInvitations))
					return &organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(tc.response),
					}, nil
				})

			op, err := client.CreateBulkByEmails(ctx, &pb.CreateBulkInvitesByEmailsRequest{
				OrgId:  orgID,
				Emails: tc.emails,
			})
			require.NoError(t, err)

			var createOp *pb.CreateInvitesBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetCreateInvitesBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			resp, ok := createOp.Result.(*pb.CreateInvitesBulkOperation_Response)
			require.True(t, ok)

			require.Equal(t, tc.expectValidEmails, functools.Map(resp.Response.CreatedInvites, (*pb.Invite).GetInviteeEmail))
			require.Equal(t, tc.expectInvalid, functools.SliceToMapKV(resp.Response.Errors, func(i *pb.InviteError) (string, string) {
				return i.InviteeEmail, i.Error.Fallback
			}))

			//yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response, protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcInvitesCreateBulk_ByID() {
	t := suite.T()

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-invites",
			Claims:     entities.OrganizationClaims{Name: "for-invites"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	// Setup users
	admin := suite.users.Admin
	user := suite.users.Barash
	failedUser := suite.users.Krosh

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	for _, tc := range []struct {
		name       string
		request    *pb.CreateBulkInvitesByIDsRequest
		setupUsers func()
		setupMock  func(*mocks.MockYCPSDKInvitationClient)
	}{
		{
			name: "happy_path",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId:   grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{grpc_marshalling.IDInverse(user.ID)},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "org_member",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId:   grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{grpc_marshalling.IDInverse(orgMember.ID)},
			},
		},
		{
			name: "federative_user",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId:   grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{grpc_marshalling.IDInverse(fedUser.ID)},
			},
		},
		{
			name: "iam_error",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId:   grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{grpc_marshalling.IDInverse(failedUser.ID)},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "all_invalid_grpc_error",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId:   grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{grpc_marshalling.IDInverse(user.ID), grpc_marshalling.IDInverse(failedUser.ID)},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, status.New(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.").Err())
			},
		},
		{
			name: "comprehensive",
			request: &pb.CreateBulkInvitesByIDsRequest{
				OrgId: grpc_marshalling.IDInverse(org.ID),
				UserIds: []string{
					grpc_marshalling.IDInverse(user.ID),       // invited successfully
					grpc_marshalling.IDInverse(failedUser.ID), // IAM error
					grpc_marshalling.IDInverse(orgMember.ID),  // already org member - error
					grpc_marshalling.IDInverse(fedUser.ID),    // federative - error
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(admin.Identity)
			client := pb.NewInviteServiceClient(suite.grpcClient)
			opClient := pb.NewOperationServiceClient(suite.grpcClient)

			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			op, err := client.CreateBulkByIDs(ctx, tc.request)
			require.NoError(t, err)

			var createOp *pb.CreateInvitesBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetCreateInvitesBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			resp, ok := createOp.Result.(*pb.CreateInvitesBulkOperation_Response)
			require.True(t, ok)

			// yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response, protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcInvitesAddToRepoBulk() {
	t := suite.T()

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-invites",
			Claims:     entities.OrganizationClaims{Name: "for-invites"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	repo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:      org.ID,
		Slug:       "for-invites",
		Visibility: entities.Visibilities.Public,
	})

	// Setup users
	admin := suite.users.Admin
	user := suite.users.Barash
	failedUser := suite.users.Krosh

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	fedUserMember := suite.users.BiBiInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUserMember.ID)
	require.NoError(t, err)
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, fedUserMember.Identity)
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		request       *pb.AddToRepoBulkRequest
		setupBindings func()
		setupMock     func(*mocks.MockYCPSDKInvitationClient)
		code          codes.Code
	}{
		{
			name: "happy_path",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(user.ID)},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "auto_add_org_member",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(orgMember.ID)},
			},
		},
		{
			name: "auto_add_org_member_federative",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(fedUserMember.ID)},
			},
		},
		{
			name: "federative_user",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(fedUser.ID)},
			},
		},
		{
			name: "inviter_self",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(admin.ID)},
			},
		},
		{
			name: "iam_error",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(failedUser.ID)},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "invitee_binding_error",
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(failedUser.ID)},
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.Invitee})
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)

				m.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					AnyTimes()
			},
		},
		{
			name: "user_binding_error",
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.User})
			},
			request: &pb.AddToRepoBulkRequest{
				RepoId:  grpc_marshalling.IDInverse(repo.ID),
				Role:    utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{grpc_marshalling.IDInverse(orgMember.ID)},
			},
		},
		{
			name: "comprehensive",
			request: &pb.AddToRepoBulkRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{
					grpc_marshalling.IDInverse(admin.ID),      // inviter self - error
					grpc_marshalling.IDInverse(user.ID),       // invited successfully
					grpc_marshalling.IDInverse(failedUser.ID), // IAM error
					grpc_marshalling.IDInverse(orgMember.ID),  // auto-added (already org member)
					grpc_marshalling.IDInverse(fedUser.ID),    // federative - error
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "comprehensive_binding_error",
			request: &pb.AddToRepoBulkRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				UserIds: []string{
					grpc_marshalling.IDInverse(admin.ID),                // inviter self - error
					grpc_marshalling.IDInverse(user.ID),                 // invited successfully
					grpc_marshalling.IDInverse(suite.users.Slowpoke.ID), // invited successfully
					grpc_marshalling.IDInverse(failedUser.ID),           // IAM error
					grpc_marshalling.IDInverse(orgMember.ID),            // auto-added (already org member)
					grpc_marshalling.IDInverse(fedUser.ID),              // federative - error
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: user.Identity.ID,
										},
									},
								},
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: suite.users.Slowpoke.Identity.ID,
										},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)

				m.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					Times(2)
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.User})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			if tc.setupBindings != nil {
				tc.setupBindings()
				defer suite.accessBindingsService.SetForbiddenSubjectTypes(nil)
			}

			ctx := testutils.AuthorizeGRPC(admin.Identity)
			client := pb.NewInviteServiceClient(suite.grpcClient)
			opClient := pb.NewOperationServiceClient(suite.grpcClient)

			op, err := client.AddToRepoBulk(ctx, tc.request)
			require.NoError(t, err)

			var createOp *pb.AddToRepoBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetAddToRepoBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			if createOp.Metadata.Status == pb.OperationMetadata_FAILED {
				// for debug
				exc, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Error)
				require.True(t, ok)
				require.Zero(t, exc.Error.Code, exc.Error.Message)
			}

			resp, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Response)
			require.True(t, ok)

			// yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response, protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcInvitesAddToRepoBulkByEmails() {
	t := suite.T()

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-invites-by-email",
			Claims:     entities.OrganizationClaims{Name: "for-invites-by-email"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	repo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:      org.ID,
		Slug:       "for-invites-by-email",
		Visibility: entities.Visibilities.Public,
	})

	admin := suite.users.Admin

	for _, tc := range []struct {
		name          string
		request       *pb.AddToRepoBulkByEmailsRequest
		setupBindings func()
		setupMock     func(*mocks.MockYCPSDKInvitationClient)
	}{
		{
			name: "happy_path",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				Emails: []string{"hello1@world.com", "hello2@world.com"},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_CREATING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "with_iam_errors",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_VIEWER),
				Emails: []string{"hello1@world.com", "hello2@world.com"},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "all_invalid",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				Emails: []string{"hello1@world.com", "hello2@world.com"},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, status.New(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.").Err())
			},
		},
		{
			name: "invitee_binding_error",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				Emails: []string{"hello1@world.com"},
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.Invitee})
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
									},
								},
							},
						}),
					}, nil)

				m.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					AnyTimes()
			},
		},
		{
			name: "comprehensive",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				Emails: []string{
					"hello1@world.com", // invited successfully
					"hello2@world.com", // IAM error
					"hello3@world.com", // invited successfully
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_CREATING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello3@world.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
									},
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "comprehensive_binding_error",
			request: &pb.AddToRepoBulkByEmailsRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   utils.PtrFromValue(pb.RepoRole_REPO_ROLE_DEVELOPER),
				Emails: []string{
					"hello1@world.com", // invited successfully but binding error
					"hello2@world.com", // IAM error
					"hello3@world.com", // invited successfully but binding error
				},
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.Invitee})
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello1@world.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_CREATING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello3@world.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "hello2@world.com"},
									},
								},
							},
						}),
					}, nil)

				m.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					Times(2)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			if tc.setupBindings != nil {
				tc.setupBindings()
				defer suite.accessBindingsService.SetForbiddenSubjectTypes(nil)
			}

			ctx := testutils.AuthorizeGRPC(admin.Identity)
			client := pb.NewInviteServiceClient(suite.grpcClient)
			opClient := pb.NewOperationServiceClient(suite.grpcClient)

			op, err := client.AddToRepoBulkByEmails(ctx, tc.request)
			require.NoError(t, err)

			var createOp *pb.AddToRepoBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetAddToRepoBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			if createOp.Metadata.Status == pb.OperationMetadata_FAILED {
				// for debug
				exc, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Error)
				require.True(t, ok)
				require.Zero(t, exc.Error.Code, exc.Error.Message)
			}

			resp, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Response)
			require.True(t, ok)

			// yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response, protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcInvitesCreateBulk() {
	t := suite.T()

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-bulk-invites",
			Claims:     entities.OrganizationClaims{Name: "for-bulk-invites"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	// Setup users
	admin := suite.users.Admin
	user := suite.users.Barash
	failedUser := suite.users.Krosh

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	fedUserMember := suite.users.BiBiInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUserMember.ID)
	require.NoError(t, err)
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, fedUserMember.Identity)
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		request   *pb.CreateBulkInvitesRequest
		setupMock func(*mocks.MockYCPSDKInvitationClient)
	}{
		{
			name: "happy_path",
			request: &pb.CreateBulkInvitesRequest{
				OrgId: grpc_marshalling.IDInverse(org.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "test@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "my-invite-code"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "test@example.com"},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:        "invite-user-1",
									Status:    organizationmanager.Invitation_PENDING,
									InviteeId: "invitee-subject-id",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: user.Identity.ID},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.ValidInvitation{
								{
									Invitation: &organizationmanager.Invitation{
										Id:        "invite-alias-1",
										Status:    organizationmanager.Invitation_PENDING,
										InviteeId: "inviteeId",
										Identity: &organizationmanager.Invitation_Anonymous_{
											Anonymous: &organizationmanager.Invitation_Anonymous{Name: "my-invite-code"},
										},
									},
									Code: "secret-invite-code-123",
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "partial_success",
			request: &pb.CreateBulkInvitesRequest{
				OrgId: grpc_marshalling.IDInverse(org.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "valid@example.com"}}},
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "invalid@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(failedUser.ID)}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(orgMember.ID)}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(fedUser.ID)}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(fedUserMember.ID)}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "valid-alias"}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "invalid-alias"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "valid@example.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Id: "invite-email-invalid",
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "invalid@example.com"},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-by-id",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: user.Identity.ID},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Id: "invite-by-id-invalid",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: failedUser.Identity.ID},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.ValidInvitation{
								{
									Invitation: &organizationmanager.Invitation{
										Id:        "invite-by-alias",
										Status:    organizationmanager.Invitation_PENDING,
										InviteeId: "inviteeId",
										Identity: &organizationmanager.Invitation_Anonymous_{
											Anonymous: &organizationmanager.Invitation_Anonymous{Name: "valid-alias"},
										},
									},
									Code: "secret-code-456",
								},
							},
							InvalidInvitations: []*organizationmanager.InvalidInvitation{
								{
									UserToInvite: &organizationmanager.UserToInvite{
										Identity: &organizationmanager.UserToInvite_Anonymous_{
											Anonymous: &organizationmanager.UserToInvite_Anonymous{
												Name: "invalid-alias",
											},
										},
									},
									Reason: organizationmanager.InvalidInvitation_INVITATION_ALREADY_EXISTS,
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "all_invalid_grpc_error",
			request: &pb.CreateBulkInvitesRequest{
				OrgId: grpc_marshalling.IDInverse(org.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "error@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "invalid-alias"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr).Times(2)

				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							InvalidInvitations: []*organizationmanager.InvalidInvitation{
								{
									UserToInvite: &organizationmanager.UserToInvite{
										Identity: &organizationmanager.UserToInvite_Anonymous_{
											Anonymous: &organizationmanager.UserToInvite_Anonymous{
												Name: "invalid-alias",
											},
										},
									},
									Reason: organizationmanager.InvalidInvitation_INVITATION_ALREADY_EXISTS,
								},
							},
						}),
					}, nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

				tc.setupMock(cloudMock)
			}

			ctx := testutils.AuthorizeGRPC(admin.Identity)
			client := pb.NewInviteServiceClient(suite.grpcClient)
			opClient := pb.NewOperationServiceClient(suite.grpcClient)

			op, err := client.CreateBulk(ctx, tc.request)
			require.NoError(t, err)

			var createOp *pb.CreateInvitesBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetCreateInvitesBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			if createOp.Metadata.Status == pb.OperationMetadata_FAILED {
				exc, ok := createOp.Result.(*pb.CreateInvitesBulkOperation_Error)
				require.True(t, ok)
				require.Zero(t, exc.Error.Code, exc.Error.Message)
			}

			resp, ok := createOp.Result.(*pb.CreateInvitesBulkOperation_Response)
			require.True(t, ok)

			slices.SortFunc(resp.Response.CreatedInvites, func(a, b *pb.Invite) int {
				if a.InviteeEmail != b.InviteeEmail {
					return strings.Compare(a.InviteeEmail, b.InviteeEmail)
				}
				if a.InviteeAlias != b.InviteeAlias {
					return strings.Compare(a.InviteeAlias, b.InviteeAlias)
				}
				return strings.Compare(a.InviteeId, b.InviteeId)
			})
			slices.SortFunc(resp.Response.Errors, func(a, b *pb.InviteError) int {
				if a.InviteeEmail != b.InviteeEmail {
					return strings.Compare(a.InviteeEmail, b.InviteeEmail)
				}
				if a.InviteeAlias != b.InviteeAlias {
					return strings.Compare(a.InviteeAlias, b.InviteeAlias)
				}
				return strings.Compare(a.InviteeId, b.InviteeId)
			})

			// yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response,
				protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcInvitesCreateBulkToRepo() {
	t := suite.T()

	admin := suite.users.Slowpoke

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-bulk-invites-to-repo",
			Claims:     entities.OrganizationClaims{Name: "for-bulk-invites-to-repo"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	repo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		OrgID:      org.ID,
		Slug:       "for-bulk-invites-to-repo",
		Visibility: entities.Visibilities.Public,
	})

	// Setup users
	user := suite.users.Barash
	failedUser := suite.users.Krosh

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	fedUserMember := suite.users.BiBiInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUserMember.ID)
	require.NoError(t, err)
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, fedUserMember.Identity)
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		request       *pb.CreateBulkRepoInvitesRequest
		setupBindings func()
		setupMock     func(*mocks.MockYCPSDKInvitationClient)
		code          codes.Code
	}{
		{
			name: "happy_path",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   pb.RepoRole_REPO_ROLE_DEVELOPER,
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "test@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "my-invite-code"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "test@example.com"},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:        "invite-user-1",
									Status:    organizationmanager.Invitation_PENDING,
									InviteeId: "invitee-subject-id",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: user.Identity.ID},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.ValidInvitation{
								{
									Invitation: &organizationmanager.Invitation{
										Id:        "invite-alias-1",
										Status:    organizationmanager.Invitation_PENDING,
										InviteeId: "inviteeId",
										Identity: &organizationmanager.Invitation_Anonymous_{
											Anonymous: &organizationmanager.Invitation_Anonymous{Name: "my-invite-code"},
										},
									},
									Code: "secret-invite-code-123",
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "all_invalid_grpc_error",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   pb.RepoRole_REPO_ROLE_CONTRIBUTOR,
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "error@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "invalid-alias"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				grpcErr := status.Error(codes.InvalidArgument, "No valid emails or user accounts. Or all specified emails and user accounts have been invited before.")
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(nil, grpcErr).Times(2)

				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							InvalidInvitations: []*organizationmanager.InvalidInvitation{
								{
									UserToInvite: &organizationmanager.UserToInvite{
										Identity: &organizationmanager.UserToInvite_Anonymous_{
											Anonymous: &organizationmanager.UserToInvite_Anonymous{
												Name: "invalid-alias",
											},
										},
									},
									Reason: organizationmanager.InvalidInvitation_INVITATION_ALREADY_EXISTS,
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "comprehensive",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "valid@example.com"}}},
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "invalid@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(admin.ID)}}},      // inviter self - error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},       // invited successfully
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(failedUser.ID)}}}, // IAM error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(orgMember.ID)}}},  // auto-added (already org member)
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(fedUser.ID)}}},    // federative - error
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "valid-alias"}}},
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "invalid-alias"}}},
				},
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "valid@example.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email-2",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "invalid@example.com"},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-user-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: user.Identity.ID},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Id: "invite-user-failed",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: failedUser.Identity.ID},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.ValidInvitation{
								{
									Invitation: &organizationmanager.Invitation{
										Id:        "valid-alias",
										Status:    organizationmanager.Invitation_PENDING,
										InviteeId: "inviteeId",
										Identity: &organizationmanager.Invitation_Anonymous_{
											Anonymous: &organizationmanager.Invitation_Anonymous{Name: "my-invite-code"},
										},
									},
									Code: "secret-invite-code-123",
								},
							},
							InvalidInvitations: []*organizationmanager.InvalidInvitation{
								{
									UserToInvite: &organizationmanager.UserToInvite{
										Identity: &organizationmanager.UserToInvite_Anonymous_{
											Anonymous: &organizationmanager.UserToInvite_Anonymous{
												Name: "invalid-alias",
											},
										},
									},
									Reason: organizationmanager.InvalidInvitation_INVITATION_ALREADY_EXISTS,
								},
							},
						}),
					}, nil)
			},
		},
		{
			name: "comprehensive_binding_error",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Role:   pb.RepoRole_REPO_ROLE_DEVELOPER,
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "test@example.com"}}},
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(admin.ID)}}},                // inviter self - error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(user.ID)}}},                 // invited successfully but binding error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(suite.users.Slowpoke.ID)}}}, // invited successfully but binding error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(failedUser.ID)}}},           // IAM error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(orgMember.ID)}}},            // auto-added but binding error
					{Identity: &pb.UserToInvite_User{User: &pb.UserToInvite_UserAccount{Id: grpc_marshalling.IDInverse(fedUser.ID)}}},              // federative - error
					{Identity: &pb.UserToInvite_Code{Code: &pb.UserToInvite_InviteCode{Alias: "my-invite-code"}}},
				},
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.User, entities.Subjects.Invitee})
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-email-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "test@example.com"},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					Create(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Id:     "invite-user-1",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: user.Identity.ID},
									},
								},
								{
									Id:     "invite-user-2",
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: suite.users.Slowpoke.Identity.ID},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Id: "invite-user-failed",
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{Id: failedUser.Identity.ID},
									},
								},
							},
						}),
					}, nil)
				cloudMock.EXPECT().
					BulkCreate(gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationBulkCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.BulkCreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.ValidInvitation{
								{
									Invitation: &organizationmanager.Invitation{
										Id:        "invite-alias-1",
										Status:    organizationmanager.Invitation_PENDING,
										InviteeId: "inviteeId",
										Identity: &organizationmanager.Invitation_Anonymous_{
											Anonymous: &organizationmanager.Invitation_Anonymous{Name: "my-invite-code"},
										},
									},
									Code: "secret-invite-code-123",
								},
							},
						}),
					}, nil)
				// Rollback invites due to binding errors
				cloudMock.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					Times(4) // email invite + 2 user invites + code invite
			},
		},
		{
			name: "permission_denied",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "test@example.com"}}},
				},
			},
			code: codes.PermissionDenied,
		},
		{
			name: "not_found",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: "99999",
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "test@example.com"}}},
				},
			},
			code: codes.NotFound,
		},
		{
			name: "empty_invites",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
			},
			code: codes.InvalidArgument,
		},
		{
			name: "invalid_email",
			request: &pb.CreateBulkRepoInvitesRequest{
				RepoId: grpc_marshalling.IDInverse(repo.ID),
				Invitees: []*pb.UserToInvite{
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: "some"}}},
					{Identity: &pb.UserToInvite_Email_{Email: &pb.UserToInvite_Email{Email: ""}}},
				},
			},
			code: codes.InvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

				tc.setupMock(cloudMock)
			}

			if tc.setupBindings != nil {
				tc.setupBindings()
				defer suite.accessBindingsService.SetForbiddenSubjectTypes(nil)
			}

			ctx := testutils.AuthorizeGRPC(admin.Identity)
			client := pb.NewInviteServiceClient(suite.grpcClient)
			opClient := pb.NewOperationServiceClient(suite.grpcClient)

			op, err := client.CreateBulkToRepo(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			var createOp *pb.AddToRepoBulkOperation
			require.Eventuallyf(t, func() bool {
				createOp, err = opClient.GetAddToRepoBulk(ctx, &pb.GetOperationRequest{Id: op.Id})
				require.NoError(t, err)
				return createOp.Done
			}, time.Second*20, 50*time.Millisecond, "operation not finished")

			if createOp.Metadata.Status == pb.OperationMetadata_FAILED {
				exc, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Error)
				require.True(t, ok)
				require.Zero(t, exc.Error.Code, exc.Error.Message)
			}

			resp, ok := createOp.Result.(*pb.AddToRepoBulkOperation_Response)
			require.True(t, ok)

			slices.SortFunc(resp.Response.CreatedInvites, func(a, b *pb.Invite) int {
				if a.InviteeEmail != b.InviteeEmail {
					return strings.Compare(a.InviteeEmail, b.InviteeEmail)
				}
				if a.InviteeAlias != b.InviteeAlias {
					return strings.Compare(a.InviteeAlias, b.InviteeAlias)
				}
				return strings.Compare(a.InviteeId, b.InviteeId)
			})
			slices.SortFunc(resp.Response.Errors, func(a, b *pb.InviteError) int {
				if a.InviteeEmail != b.InviteeEmail {
					return strings.Compare(a.InviteeEmail, b.InviteeEmail)
				}
				if a.InviteeAlias != b.InviteeAlias {
					return strings.Compare(a.InviteeAlias, b.InviteeAlias)
				}
				return strings.Compare(a.InviteeId, b.InviteeId)
			})
			slices.Sort(resp.Response.AddedUserIds)

			// yarequire.ProtoDumpFixture(t, resp.Response)
			yarequire.ProtoCompareWithFixture(t, resp.Response,
				protocmp.IgnoreFields(&pb.Invite{}, "expires_at", "created_at"),
			)
		})
	}
}
