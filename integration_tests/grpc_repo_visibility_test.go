package integrationtests

import (
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestGrpcUpdateRepoVisibilityTest() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	alpha := suite.repos.Alpha

	reposPub, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesCount)
	require.NoError(t, err)
	reposPriv, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesPrivateCount)
	require.NoError(t, err)
	storagePub, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	storagePriv, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStoragePrivateSize)
	require.NoError(t, err)

	resp, err := client.Update(ctx, &pb.UpdateRepositoryRequest{
		Id:         grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		Visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)

	require.Equal(t, pb.ResourceVisibility_RESOURCE_PRIVATE, repo.Visibility)

	reposPub2, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesCount)
	require.NoError(t, err)
	reposPriv2, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesPrivateCount)
	require.NoError(t, err)
	storagePub2, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	storagePriv2, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStoragePrivateSize)
	require.NoError(t, err)

	require.Equal(t, reposPub.Usage-1, reposPub2.Usage)
	require.Equal(t, reposPriv.Usage+1, reposPriv2.Usage)
	require.Less(t, storagePub2.Usage, storagePub.Usage)
	require.Greater(t, storagePriv2.Usage, storagePriv.Usage)

	resp, err = client.Update(ctx, &pb.UpdateRepositoryRequest{
		Id:         grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
	})
	require.NoError(t, err)

	_, err = grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)

	reposPub3, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesCount)
	require.NoError(t, err)
	reposPriv3, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.RepositoriesPrivateCount)
	require.NoError(t, err)
	storagePub3, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	storagePriv3, err := suite.quotaService.Get(ctx, alpha.OrgID, entities.Quotas.ObjectStoragePrivateSize)
	require.NoError(t, err)

	require.Equal(t, reposPub2.Usage, reposPub3.Usage)
	require.Equal(t, reposPriv2.Usage, reposPriv3.Usage)
	require.Equal(t, storagePub2.Usage, storagePub3.Usage)
	require.Equal(t, storagePriv2.Usage, storagePriv3.Usage)
}
