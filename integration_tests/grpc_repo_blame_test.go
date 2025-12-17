package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGrpcRepoBlame_Small() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	tests := []struct {
		name string
		rev  string
		path string
		from uint32
		to   uint32
	}{
		{
			name: "rename",
			rev:  "master",
			path: "file-rename.txt",
		},
		{
			name: "insert and delete",
			rev:  "master",
			path: "file3.txt",
		},
		{
			name: "line filter",
			rev:  "master",
			path: "file3.txt",
			from: 5,
			to:   7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blameRange *pb.TextFileRange
			if tt.from != 0 && tt.to != 0 {
				blameRange = &pb.TextFileRange{
					From: tt.from,
					To:   tt.to,
				}
			}
			resp, err := client.Blame(ctx, &pb.BlameRequest{
				Id:    grpc.MarshalID(suite.repos.Blame.ID),
				Rev:   tt.rev,
				Path:  tt.path,
				Range: blameRange,
			})
			require.NoError(t, err)
			checkCommits(t, resp)

			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}

func (suite *LargeRepoApiTestSuite) TestGrpcRepoBlame_LargeWithOdyssey() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesContributor)

	odyssey, err := client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_FullSlug{
		FullSlug: &pb.RepositoryFullSlug{
			OrgSlug:  "yandex",
			RepoSlug: "odyssey",
		}}})
	require.NoError(t, err)

	tests := []struct {
		name string
		rev  string
		path string
	}{
		{
			name: ".gitignore",
			rev:  "9d0de5d4e5ef1c3489570497c847845a95be7dc9",
			path: ".gitignore",
		},
		{
			name: "sources/CMakeLists.txt",
			rev:  "9d0de5d4e5ef1c3489570497c847845a95be7dc9",
			path: "sources/CMakeLists.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Blame(ctx, &pb.BlameRequest{
				Id:   odyssey.Id,
				Rev:  tt.rev,
				Path: tt.path,
			})

			require.NoError(t, err)
			checkCommits(t, resp)

			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}

func checkCommits(t *testing.T, resp *pb.BlameResponse) {
	commitHashes := functools.Map(resp.Commits, func(commit *pb.Commit) string {
		return commit.Hash
	})
	commitHashes = functools.Unique(commitHashes)

	for i, hash := range commitHashes {
		require.Equal(t, hash, resp.Commits[i].Hash)
	}
}
