package integrationtests

import (
	"context"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcGetRepoStatsTest() {
	t := suite.T()
	t.Skip("the flapping test")

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	require.NoError(t, suite.RepoService.WaitForMaintenance(context.Background(), suite.repos.Index.ID))

	t.Run("happy path - by ID", func(t *testing.T) {
		repo, err := client.GetRepoStatistics(ctx, &pb.GetRepositoryStatisticsRequest{
			Id: grpc_marshalling.IDInverse(suite.repos.Index.ID),
		})
		require.NoError(t, err)
		require.Len(t, repo.Flavors, 3)
		require.Equal(t, "Go", repo.Flavors[0].Language.Name)
		require.Greater(t, 60.0, repo.Flavors[0].Percentage)
		require.Equal(t, "TypeScript", repo.Flavors[1].Language.Name)
		require.Equal(t, "HTML", repo.Flavors[2].Language.Name)
	})
}
