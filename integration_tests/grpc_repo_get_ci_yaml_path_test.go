package integrationtests

import (
	"common/cgit"
	"common/oyaml"
	"common/testutils/yarequire"
	"context"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGetCiYamlPath() {
	t := suite.T()
	ctx := context.Background()

	repo := suite.repos.Alpha

	branch := "branch2"
	suite.addCommits(t, suite.RepoURL(repo.OrgSlug, repo.Slug), suite.users.Admin,
		commitsOptions{
			initializeFunc: func(t testing.TB, cg *cgit.CGit) {
				cg.Must(t, "checkout", "-b", branch)
			},
		})

	client := pb.NewRepoServiceClient(suite.grpcClient)

	t.Run("no config", func(t *testing.T) {
		_, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id: grpc_marshalling.IDInverse(repo.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("old path", func(t *testing.T) {
		suite.addDefaultOYaml(repo, "master", oyaml.OldPath)

		resp, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id: grpc_marshalling.IDInverse(repo.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		require.Equal(t, oyaml.OldPath, resp.Path)
	})

	t.Run("no new ci path but other new path exists", func(t *testing.T) {
		suite.addDefaultOYaml(repo, "master", oyaml.ReviewPath)

		_, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id: grpc_marshalling.IDInverse(repo.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("new path", func(t *testing.T) {
		suite.addDefaultOYaml(repo, "master", oyaml.CIPath)

		resp, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id: grpc_marshalling.IDInverse(repo.ID),
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		require.Equal(t, oyaml.CIPath, resp.Path)
	})

	t.Run("by rev - no config", func(t *testing.T) {
		_, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id:  grpc_marshalling.IDInverse(repo.ID),
			Rev: branch,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("by rev - old path", func(t *testing.T) {
		suite.addDefaultOYaml(repo, branch, oyaml.OldPath)

		resp, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id:  grpc_marshalling.IDInverse(repo.ID),
			Rev: branch,
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		require.Equal(t, oyaml.OldPath, resp.Path)
	})

	t.Run("by rev - no new ci path but other new path exists", func(t *testing.T) {
		suite.addDefaultOYaml(repo, branch, oyaml.ReviewPath)

		_, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id:  grpc_marshalling.IDInverse(repo.ID),
			Rev: branch,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("by rev - new path", func(t *testing.T) {
		suite.addDefaultOYaml(repo, branch, oyaml.CIPath)

		resp, err := client.GetCIYamlPath(ctx, &pb.GetCIYamlPathRequest{
			Id:  grpc_marshalling.IDInverse(repo.ID),
			Rev: branch,
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		require.Equal(t, oyaml.CIPath, resp.Path)
	})
}
