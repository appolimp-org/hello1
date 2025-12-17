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
	"time"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestResendInviteGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	o := &organizationmanagersdk.InvitationResendOperation{
		Operation: suite.prepareSDKResponse(&organizationmanager.ResendInvitationResponse{
			InvitationId: "cloud-invite-id",
		}),
	}
	cloudMock.EXPECT().Resend(gomock.Any(), gomock.Any(), gomock.Any()).Return(o, nil)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.OrganizationManagerOrganizationsOwner},
	}))

	op, err := client.Resend(ctx, &pb.ResendInviteRequest{
		OrgId:     orgID,
		InviteId:  "cloud-invite-id",
		TtlInDays: nil,
	})
	require.NoError(t, err)

	var createOp *pb.ResendInviteOperation

	require.Eventuallyf(t, func() bool {
		createOp, err = opClient.GetResendInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
		require.NoError(t, err)
		return createOp.Done
	}, time.Second*20, 50*time.Millisecond, "operation not finished")

	_, ok := createOp.Result.(*pb.ResendInviteOperation_Response)
	require.True(t, ok)
}
