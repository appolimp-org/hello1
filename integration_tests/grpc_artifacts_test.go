package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpcMarshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/artifacts"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestRepoArtifacts() {
	suite.testArtifacts(true)
}

func (suite *RwApiTestSuite) TestProjArtifacts() {
	suite.testArtifacts(false)
}

func (suite *RwApiTestSuite) testArtifacts(isRepo bool) {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	var entityID uint64
	var err error
	var entityType entities.EntityType
	repo := suite.RepoFixture(0, suite.orgs.Yandex42, "test-repo", testutils.BasicRepo, nil)
	if isRepo {
		entityID = repo.ID
		entityType = entities.EntityTypes.Repository
	} else {
		entityType = entities.EntityTypes.Project
		entityID, err = suite.MakeProject(repo.OrgSlug, "goodproj", entities.Visibilities.Public)
		require.NoError(t, err)
	}

	client := pb.NewAssociatedRegistryServiceClient(suite.grpcClient)
	artifactsRegistryClient := pb.NewArtifactsRegistryServiceClient(suite.grpcClient)
	suite.addExternalOrgRole(t, suite.users.Admin, suite.orgs.Yandex42, iam.Roles.InternalOrganizationManagerMember)
	suite.addExternalOrgRole(t, suite.users.Admin, suite.orgs.Yandex42, iam.Roles.RepositoriesAdmin)
	suite.addExternalOrgRole(t, suite.users.Admin, suite.orgs.Yandex42, iam.Roles.ProjectsAdmin)

	name := "test-cloud-registry"
	pbCloudRegistry := &pb.CloudRegistry{
		Name: &name,
		Kind: pb.CloudRegistry_KIND_NPM,
		Type: pb.CloudRegistry_TYPE_LOCAL,
	}
	orgID := strconv.FormatUint(suite.orgs.Yandex42.ID, 10)
	_, err = artifactsRegistryClient.SetupCloudRegistry(ctx, &pb.SetupCloudRegistryRequest{
		OrganizationId: orgID,
		CloudRegistry:  pbCloudRegistry,
	})
	require.NoError(t, err)
	suite.WaitForWorkflows(t, artifacts.ProvisionArtifactsEnvWorkflowType)
	assocOrgRegistries, err := artifactsRegistryClient.ListArtifactRegistries(ctx, &pb.ListArtifactRegistriesRequest{
		OrganizationId: orgID,
	})
	require.NoError(t, err)
	orgRegistry := assocOrgRegistries.ArtifactRegistries[0]

	orgRegistryResp, err := artifactsRegistryClient.GetArtifactRegistry(ctx, &pb.GetArtifactRegistryRequest{
		OrgRegistryId: orgRegistry.Id,
	})
	require.NoError(t, err)
	yarequire.ProtoEqual(t, orgRegistry, orgRegistryResp)

	_, err = artifactsRegistryClient.GetArtifactRegistryByOrgSlugAndRegistry(ctx, &pb.GetArtifactRegistryByOrgSlugAndRegistryRequest{
		OrganizationSlug: suite.orgs.Yandex42.Slug,
		RegistryId:       "test-cloud-registry",
	})
	require.Error(t, err)

	associateReq := &pb.AssociateRegistryRequest{
		EntityId:      grpcMarshalling.IDInverse(entityID),
		EntityType:    grpcMarshalling.EntityTypeInverse(entityType),
		OrgRegistryId: orgRegistry.Id,
	}

	associateOp, err := client.AssociateRegistry(ctx, associateReq)
	require.NoError(t, err)

	associateMeta, err := associateOp.Metadata.UnmarshalNew()
	require.NoError(t, err)
	yarequire.ProtoEqual(t, associateReq, associateMeta)

	artifacts := testutils.UnmarshalGrpcResult[*pb.AssociatedRegistry](t, associateOp)
	require.NotNil(t, artifacts)
	require.NotEqual(t, "", artifacts.GetId())
	require.Equal(t, grpcMarshalling.IDInverse(entityID), artifacts.GetEntityId())

	gettedArtifacts, err := client.GetAssociatedRegistryInfo(ctx, &pb.GetAssociatedRegistryInfoRequest{Id: artifacts.GetId()})
	require.NoError(t, err)
	yarequire.ProtoEqual(t, artifacts, gettedArtifacts)

	assocRegs, err := client.ListAssociatedRegistriesByArtifactRegistry(ctx, &pb.ListAssociatedRegistriesByArtifactRegistryRequest{
		OrgRegistryId: orgRegistry.Id,
	})
	require.NoError(t, err)
	require.Len(t, assocRegs.AssociatedRegistries, 1)
	yarequire.ProtoEqual(t, gettedArtifacts, assocRegs.AssociatedRegistries[0])

	listedArtifacts, err := client.ListAssociatedRegistries(ctx, &pb.ListAssociatedRegistriesRequest{
		EntityId:   grpcMarshalling.IDInverse(entityID),
		EntityType: grpcMarshalling.EntityTypeInverse(entityType),
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(listedArtifacts.Registries))
	yarequire.ProtoEqual(t, artifacts, listedArtifacts.Registries[0])

	_, err = artifactsRegistryClient.AuthorizeProxyCall(ctx, &pb.AuthorizeProxyCallRequest{
		OrgRegistryId: orgRegistry.Id,
		Permission:    string(iam.Permissions.RepositoriesGetArtifactsRegistry),
	})
	require.NoError(t, err)

	forgetReq := &pb.ForgetAssociatedRegistryRequest{AssociatedRegistryId: artifacts.GetId()}

	forgetOp, err := client.ForgetAssociatedRegistry(ctx, forgetReq)
	require.NoError(t, err)

	forgetMeta, err := forgetOp.Metadata.UnmarshalNew()
	require.NoError(t, err)
	yarequire.ProtoEqual(t, forgetReq, forgetMeta)

	_, err = client.GetAssociatedRegistryInfo(ctx, &pb.GetAssociatedRegistryInfoRequest{Id: artifacts.GetId()})
	yarequire.ProtoStatusEqual(t, codes.NotFound, err)

	_, err = artifactsRegistryClient.ForgetArtifactRegistry(ctx, &pb.ForgetArtifactRegistryRequest{
		OrgRegistryId: orgRegistry.Id,
	})
	require.NoError(t, err)
}
