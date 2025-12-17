package integrationtests

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/services/artifacts"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) setupTestCloudRegistry(ctx context.Context, orgID string, registry *pb.CloudRegistry) *pb.ArtifactRegistry {
	t := suite.T()
	client := pb.NewArtifactsRegistryServiceClient(suite.grpcClient)

	_, err := client.SetupCloudRegistry(ctx, &pb.SetupCloudRegistryRequest{
		OrganizationId: orgID,
		CloudRegistry:  registry,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, artifacts.ProvisionArtifactsEnvWorkflowType)

	assocOrgRegistries, err := client.ListArtifactRegistries(ctx, &pb.ListArtifactRegistriesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)

	cloudRegistries, err := client.ListCloudRegistries(ctx, &pb.ListCloudRegistriesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)

	cloudRegistryID := ""
	for _, cloudRegistry := range cloudRegistries.CloudRegistries {
		if cloudRegistry.Name == nil {
			continue
		}
		if *cloudRegistry.Name == *registry.Name {
			cloudRegistryID = cloudRegistry.Id
			break
		}
	}

	if cloudRegistryID == "" {
		return nil
	}

	for _, orgRegistry := range assocOrgRegistries.ArtifactRegistries {
		if orgRegistry.CloudRegistryId == cloudRegistryID {
			return orgRegistry
		}
	}
	return nil
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOnboardCloudRegistry() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewArtifactsRegistryServiceClient(suite.grpcClient)

	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.RepositoriesAdmin)
	orgID := strconv.FormatUint(suite.orgs.Yandex42.ID, 10)

	name := "test-cloud-registry0"
	pbCloudRegistry := &pb.CloudRegistry{
		Name: &name,
		Kind: pb.CloudRegistry_KIND_NPM,
		Type: pb.CloudRegistry_TYPE_LOCAL,
	}

	tt := map[string]struct {
		user           *entities.User
		request        *pb.SetupCloudRegistryRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"registry created": {
			user: suite.users.Kopatych,
			request: &pb.SetupCloudRegistryRequest{
				OrganizationId: orgID,
				CloudRegistry:  pbCloudRegistry,
			},
		},
		"registry create permission denied error": {
			user: suite.users.Barash,
			request: &pb.SetupCloudRegistryRequest{
				OrganizationId: orgID,
				CloudRegistry:  pbCloudRegistry,
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.SetupCloudRegistry(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			suite.WaitForWorkflows(t, artifacts.ProvisionArtifactsEnvWorkflowType)

			msgs, err := et.GetOnboardCloudRegistryAuditEvents(ctx)
			require.NoError(t, err)
			require.True(t, len(msgs) > 0)

			for idx, msg := range msgs {
				t.Run("msg_"+strconv.Itoa(idx), func(t *testing.T) {
					//yarequire.ProtoDumpFixture(t, msg)
					yarequire.ProtoCompareWithFixture(t, msg, et.ProtoCompareOpts()...)

					et.HasEventMetaDataOrganizationID(t, msg)
				})
			}
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOffboardCloudRegistry() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewArtifactsRegistryServiceClient(suite.grpcClient)

	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.RepositoriesAdmin)
	orgID := strconv.FormatUint(suite.orgs.Yandex42.ID, 10)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	name1 := "test-cloud-registry1"
	pbCloudRegistry1 := &pb.CloudRegistry{
		Name: &name1,
		Kind: pb.CloudRegistry_KIND_NPM,
		Type: pb.CloudRegistry_TYPE_LOCAL,
	}
	registry1 := suite.setupTestCloudRegistry(ctx, orgID, pbCloudRegistry1)

	name2 := "test-cloud-registry2"
	pbCloudRegistry2 := &pb.CloudRegistry{
		Name: &name2,
		Kind: pb.CloudRegistry_KIND_NPM,
		Type: pb.CloudRegistry_TYPE_LOCAL,
	}
	registry2 := suite.setupTestCloudRegistry(ctx, orgID, pbCloudRegistry2)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.ForgetArtifactRegistryRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"registry offboard": {
			user: suite.users.Kopatych,
			request: &pb.ForgetArtifactRegistryRequest{
				OrgRegistryId: registry1.Id,
			},
		},
		"registry offboard permission denied error": {
			user: suite.users.Barash,
			request: &pb.ForgetArtifactRegistryRequest{
				OrgRegistryId: registry2.Id,
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.ForgetArtifactRegistry(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetOffboardCloudRegistryAuditEvents(ctx)
			require.NoError(t, err)
			require.True(t, len(msgs) > 0)

			for idx, msg := range msgs {
				t.Run("msg_"+strconv.Itoa(idx), func(t *testing.T) {
					//yarequire.ProtoDumpFixture(t, msg)
					yarequire.ProtoCompareWithFixture(t, msg, et.ProtoCompareOpts()...)

					et.HasEventMetaDataOrganizationID(t, msg)
				})
			}
		})
	}
}
