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
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestCancelInviteGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)

	ctrl, reporter := testutils.NewMockController(t)
	defer reporter.Finish(ctrl)

	cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
	suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)

	o := &organizationmanagersdk.InvitationDeleteOperation{
		Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
	}
	cloudMock.EXPECT().Delete(gomock.Any(), &organizationmanager.DeleteInvitationRequest{
		InvitationId: "cloud-invite-id",
	}, gomock.Any()).Return(o, nil)

	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	orgID := grpc_marshalling.IDInverse(org.ID)
	ctx := testutils.AuthorizeGRPC(admin.Identity)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: admin.Subject(), Object: org.Object(), Role: iam.Roles.OrganizationManagerOrganizationsOwner},
	}))

	op, err := client.Cancel(ctx, &pb.CancelInviteRequest{
		OrgId:    orgID,
		InviteId: "cloud-invite-id",
	})
	require.NoError(t, err)

	var createOp *pb.CancelInviteOperation

	require.Eventuallyf(t, func() bool {
		createOp, err = opClient.GetCancelInvite(ctx, &pb.GetOperationRequest{Id: op.Id})
		require.NoError(t, err)
		return createOp.Done
	}, time.Second*20, 50*time.Millisecond, "operation not finished")

	_, ok := createOp.Result.(*pb.CancelInviteOperation_Response)
	require.True(t, ok)
}
