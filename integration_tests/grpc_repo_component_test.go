package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcComponentBase() {
	t := suite.T()
	client := pb.NewReleaseServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)

	t.Run("list components empty", func(t *testing.T) {
		list, err := client.ListComponentsByRepo(ctx, &pb.ListComponentsByRepoRequest{
			RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		})
		require.NoError(t, err)
		require.Len(t, list.Components, 0)
	})

	opts1 := &pb.CreateComponentRequest{
		RepoId:    grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		Name:      "1",
		TagPrefix: "release-",
		Path:      "help",
	}
	resp, err := client.CreateComponent(ctx, opts1)
	require.NoError(t, err)

	require.Equal(t, opts1.RepoId, resp.Component.RepoId)
	require.Equal(t, opts1.Description, resp.Component.Description)
	require.Equal(t, opts1.Name, resp.Component.Name)
	require.Equal(t, opts1.Path, resp.Component.Path)

	t.Run("get", func(t *testing.T) {
		component, err := client.GetComponent(ctx, &pb.GetComponentRequest{
			Id: resp.Component.Id,
		})

		require.NoError(t, err)
		require.Equal(t, resp.Component, component.Component)
	})

	t.Run("get non existing", func(t *testing.T) {
		_, err := client.GetComponent(ctx, &pb.GetComponentRequest{
			Id: grpc_marshalling.IDInverse(99999999),
		})

		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("list components 1", func(t *testing.T) {
		list, err := client.ListComponentsByRepo(ctx, &pb.ListComponentsByRepoRequest{
			RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
		})
		require.NoError(t, err)
		require.Len(t, list.Components, 1)
		require.Equal(t, resp.Component.Id, list.Components[0].Id)
	})

	t.Run("create already exists", func(t *testing.T) {
		_, err := client.CreateComponent(ctx, opts1)
		yarequire.ProtoStatusEqual(t, codes.AlreadyExists, err)
	})

	t.Run("invalid tag prefix - bad ref", func(t *testing.T) {
		createRequest := &pb.CreateComponentRequest{
			RepoId:    grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
			Name:      "4",
			TagPrefix: "/refs/heads/%s/",
		}

		_, err := client.CreateComponent(ctx, createRequest)
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})
}
