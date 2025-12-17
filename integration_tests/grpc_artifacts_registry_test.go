package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/services/artifacts"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestArtifactsRegistryService() {
	t := suite.T()

	artifactsRegistryClient := pb.NewArtifactsRegistryServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addExternalOrgRole(t, suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.RepositoriesAdmin)

	orgID := strconv.FormatUint(suite.orgs.Yandex42.ID, 10)

	name := "test-cloud-registry"
	pbCloudRegistry := &pb.CloudRegistry{
		Name: &name,
		Kind: pb.CloudRegistry_KIND_NPM,
		Type: pb.CloudRegistry_TYPE_LOCAL,
	}
	_, err := artifactsRegistryClient.SetupCloudRegistry(ctx, &pb.SetupCloudRegistryRequest{
		OrganizationId: orgID,
		CloudRegistry:  pbCloudRegistry,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, artifacts.ProvisionArtifactsEnvWorkflowType)

	resp, err := artifactsRegistryClient.ListCloudRegistries(ctx, &pb.ListCloudRegistriesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.CloudRegistries)
	createdRegistry := resp.CloudRegistries[0]
	require.Equal(t, pbCloudRegistry.Name, createdRegistry.Name)
	require.Equal(t, pbCloudRegistry.Type, createdRegistry.Type)
	require.Equal(t, pbCloudRegistry.Kind, createdRegistry.Kind)

	_, err = artifactsRegistryClient.ListPackages(ctx, &pb.ListPackagesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)

	assocOrgRegistries, err := artifactsRegistryClient.ListArtifactRegistries(ctx, &pb.ListArtifactRegistriesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)
	orgRegistry := assocOrgRegistries.ArtifactRegistries[0]

	packageNameInternal := "package_internal"
	packageNamePublic := "package_public"
	_, err = artifactsRegistryClient.AuthorizeProxyCallByPackage(ctx, &pb.AuthorizeProxyCallByPackageRequest{
		OrgRegistryId: orgRegistry.Id,
		PackageName:   &packageNameInternal,
	})
	require.NoError(t, err)

	_, err = artifactsRegistryClient.AuthorizeProxyCallByPackage(ctx, &pb.AuthorizeProxyCallByPackageRequest{
		OrgRegistryId: orgRegistry.Id,
		PackageName:   &packageNamePublic,
	})
	require.NoError(t, err)

	_, err = artifactsRegistryClient.ListPackageVersions(ctx, &pb.ListPackageVersionsRequest{
		OrgRegistryId: orgRegistry.Id,
		PackageName:   packageNameInternal,
	})
	require.NoError(t, err)

	_, err = artifactsRegistryClient.UpdatePackage(ctx, &pb.UpdatePackageRequest{
		OrgRegistryId: orgRegistry.Id,
		PackageName:   packageNamePublic,
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
		Visibility:    pb.Package_VISIBILITY_INTERNAL,
	})
	require.NoError(t, err)
}
