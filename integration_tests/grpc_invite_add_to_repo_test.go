package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/generated/mocks"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (suite *RwApiTestSuite) TestAddToRepoGRPC() {
	t := suite.T()

	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)

	org := suite.orgs.Yandex42
	repo := suite.ImportRepo(org, "alpha", testutils.BasicRepo, nil)
	repoID := grpc_marshalling.IDInverse(repo.ID)
	invitee := suite.users.Krosh
	inviteeID := grpc_marshalling.IDInverse(invitee.ID)

	admin := suite.users.Admin
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	role := pb.RepoRole_REPO_ROLE_DEVELOPER
	req := &pb.AddToRepoRequest{
		RepoId: repoID,
		UserId: inviteeID,
		Role:   &role,
	}

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.Admin},
	}))

	_, err := client.AddToRepo(ctx, req)
	require.Error(t, err)

	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, s.Code())

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.OrganizationManagerAdmin},
	}))

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	// test scenario when invitee is not in org

	t.Run("not a member", func(t *testing.T) {
		o := &organizationmanagersdk.InvitationCreateOperation{
			Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
				ValidInvitations: []*organizationmanager.Invitation{
					{
						Id:     "foobar",
						Status: organizationmanager.Invitation_PENDING,
						Identity: &organizationmanager.Invitation_UserAccount_{
							UserAccount: &organizationmanager.Invitation_UserAccount{Id: invitee.Identity.ID},
						},
						ServiceUri: fmt.Sprintf("%s/%s", org.Slug, repo.Slug),
					},
				},
			}),
		}
		cloudMock.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(o, nil)

		op, err := client.AddToRepo(ctx, req)
		require.NoError(t, err)

		var createOp *pb.AddToRepoOperation

		require.Eventuallyf(t, func() bool {
			createOp, err = opClient.GetAddToRepo(ctx, &pb.GetOperationRequest{Id: op.Id})
			require.NoError(t, err)
			return createOp.Done
		}, time.Second*20, 50*time.Millisecond, "operation not finished")

		resp, ok := createOp.Result.(*pb.AddToRepoOperation_Response)
		require.True(t, ok)

		require.Equal(t, inviteeID, resp.Response.GetInvite().GetInviteeId())
	})

	t.Run("member", func(t *testing.T) {
		// test scenario when invitee is in org already

		invitee = suite.users.Barash
		inviteeID = grpc_marshalling.IDInverse(invitee.ID)

		_, err = suite.SelfHostedOrgRepo.CreateOrganization( // required for stub org backend to write membership
			ctx,
			entities.IdentityProviders.IAM,
			entities.OrganizationClaims{
				Name:       "Yandex 42",
				ExternalID: org.Identity.ID,
			},
		)
		require.NoError(t, err)
		err = suite.OrgService.AddUser(ctx, nil, org.Identity, invitee.Identity)
		require.NoError(t, err)

		op, err := client.AddToRepo(ctx, &pb.AddToRepoRequest{
			RepoId: repoID,
			UserId: inviteeID,
			Role:   &role,
		})
		require.NoError(t, err)

		var createOp *pb.AddToRepoOperation

		require.Eventuallyf(t, func() bool {
			createOp, err = opClient.GetAddToRepo(ctx, &pb.GetOperationRequest{Id: op.Id})
			require.NoError(t, err)
			return createOp.Done
		}, time.Second*20, 50*time.Millisecond, "operation not finished")

		resp, ok := createOp.Result.(*pb.AddToRepoOperation_Response)
		require.True(t, ok)
		require.True(t, resp.Response.GetAdded())
	})

}
