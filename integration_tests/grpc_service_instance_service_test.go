package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "private_api/generated/yandex/cloud/priv/billing/integration/v2"
	"testing"
)

func (suite *RwApiTestSuite) TestServiceInstanceService() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	client := pb.NewServiceInstanceServiceClient(suite.grpcClient)

	org := suite.orgs.Yandex42
	serviceInstanceID := "src." + org.Identity.ID

	suite.addExternalOrgRole(t, suite.users.Kopatych, org, iam.Roles.OrganizationsInternalBillingAgent)

	require.True(t, t.Run("AuthorizeBinding", func(t *testing.T) {
		_, err := client.AuthorizeBinding(ctx, &pb.AuthorizeBindingRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
			SubjectId:           grpc_marshalling.IDInverse(suite.users.Kopatych.ID),
		})
		require.NoError(t, err)
	}))

	require.True(t, t.Run("SetStatus active", func(t *testing.T) {
		res, err := client.SetStatus(ctx, &pb.SetServiceInstanceStatusRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
			Status:              pb.ServiceInstanceStatus_SERVICE_INSTANCE_STATUS_ACTIVE,
		})
		require.NoError(t, err)
		require.True(t, res.GetDone())
	}))

	require.True(t, t.Run("Get", func(t *testing.T) {
		res, err := client.Get(ctx, &pb.GetServiceInstanceRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
		})
		require.NoError(t, err)
		require.Equal(t, res.GetOrganizationId(), org.Identity.ID)
	}))

	require.True(t, t.Run("Delete but not blocking", func(t *testing.T) {
		_, err := client.Delete(ctx, &pb.DeleteServiceInstanceRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
		})
		require.Error(t, err)
		require.Equal(t, status.Code(err), codes.PermissionDenied)
	}))

	require.True(t, t.Run("SetStatus block", func(t *testing.T) {
		res, err := client.SetStatus(ctx, &pb.SetServiceInstanceStatusRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
			Status:              pb.ServiceInstanceStatus_SERVICE_INSTANCE_STATUS_BLOCKED_HARD,
		})
		require.NoError(t, err)
		require.True(t, res.GetDone())
	}))

	require.True(t, t.Run("Delete", func(t *testing.T) {
		res, err := client.Delete(ctx, &pb.DeleteServiceInstanceRequest{
			ServiceInstanceType: "src.organization",
			ServiceInstanceId:   serviceInstanceID,
		})
		require.NoError(t, err)
		require.True(t, res.GetDone())
	}))
}
