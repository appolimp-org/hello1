package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/generated/mocks"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestListInvitesGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)
	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	cloudMock.EXPECT().ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&organizationmanager.ListOrganizationInvitationsResponse{
			Invitations: []*organizationmanager.Invitation{
				{
					Id:     "foobar",
					Status: organizationmanager.Invitation_PENDING,
					Identity: &organizationmanager.Invitation_Invitee_{
						Invitee: &organizationmanager.Invitation_Invitee{
							Email: "hello@world.com",
						},
					},
				},
				{
					Id:     "foobar2",
					Status: organizationmanager.Invitation_PENDING,
					Identity: &organizationmanager.Invitation_UserAccount_{
						UserAccount: &organizationmanager.Invitation_UserAccount{
							Id: suite.users.Krosh.Identity.ID,
						},
					},
				},
			},
			NextPageToken: "",
		}, nil)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.OrganizationManagerOrganizationsOwner},
	}))

	resp, err := client.List(ctx, &pb.ListInvitesRequest{OrgId: orgID, Status: pb.Invite_PENDING})
	require.NoError(t, err)

	require.Equal(t, 2, len(resp.Invites))

	require.Equal(t, "foobar", resp.Invites[0].Id)
	require.Equal(t, "hello@world.com", resp.Invites[0].InviteeEmail)
	require.Equal(t, pb.Invite_PENDING, resp.Invites[0].Status)

	require.Equal(t, "foobar2", resp.Invites[1].Id)
	require.Equal(t, grpc_marshalling.IDInverse(suite.users.Krosh.ID), resp.Invites[1].InviteeId)
	require.Equal(t, pb.Invite_PENDING, resp.Invites[1].Status)
}

func (suite *RwApiTestSuite) TestListIncomingInvitesGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	org := suite.orgs.Yandex42
	repo := suite.repos.Alpha
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)
	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	cloudMock.EXPECT().ListIncomingInvitations(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&organizationmanager.ListIncomingInvitationsResponse{
			Invitations: []*organizationmanager.IncomingInvitation{
				{
					Id:               "foobar",
					Status:           organizationmanager.Invitation_PENDING,
					ServiceUri:       "https://sourcecraft.dev/yandex42",
					InviterSubjectId: suite.users.Krosh.Identity.ID,
				},
				{
					Id:               "foobar2",
					Status:           organizationmanager.Invitation_PENDING,
					ServiceUri:       "https://sourcecraft.dev/yandex42/alpha",
					InviterSubjectId: suite.users.Pikachu.Identity.ID,
				},
				{
					Id:               "old_type_invitation",
					Status:           organizationmanager.Invitation_PENDING,
					ServiceUri:       "",
					InviterSubjectId: suite.users.Pikachu.Identity.ID,
				},
			},
			NextPageToken: "",
		}, nil)

	resp, err := client.ListMyIncomingInvitations(ctx, &pb.ListMyIncomingInvitationsRequest{})
	require.NoError(t, err)

	require.Equal(t, 3, len(resp.Invitations))

	require.Equal(t, "foobar", resp.Invitations[0].Id)
	require.Equal(t, suite.users.Krosh.Identity.ID, resp.Invitations[0].InviterSlug)
	require.Equal(t, org.Slug, resp.Invitations[0].OrgSlug)
	require.Nil(t, resp.Invitations[0].RepoSlug)
	require.Equal(t, pb.Invite_PENDING, resp.Invitations[0].Status)

	require.Equal(t, "foobar2", resp.Invitations[1].Id)
	require.Equal(t, suite.users.Pikachu.Identity.ID, resp.Invitations[1].InviterSlug)
	require.Equal(t, org.Slug, resp.Invitations[1].OrgSlug)
	require.NotNil(t, resp.Invitations[1].RepoSlug)
	require.Equal(t, repo.Slug, *resp.Invitations[1].RepoSlug)
	require.Equal(t, pb.Invite_PENDING, resp.Invitations[1].Status)

	require.Equal(t, "old_type_invitation", resp.Invitations[2].Id)
	require.Equal(t, suite.users.Pikachu.Identity.ID, resp.Invitations[2].InviterSlug)
	require.Equal(t, pb.Invite_PENDING, resp.Invitations[2].Status)
}
