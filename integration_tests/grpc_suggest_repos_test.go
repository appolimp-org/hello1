package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestSuggestRepos() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	org := suite.createOrg(t, suite.users.Krosh, "org")

	var repos []*pb.Repository
	for i := range 11 {
		visibility := pb.ResourceVisibility_RESOURCE_PUBLIC
		if i%2 == 0 {
			visibility = pb.ResourceVisibility_RESOURCE_PRIVATE
		}
		repo, _ := suite.createRepo(t, suite.users.Krosh, fmt.Sprintf("repo-%d", i), visibility, org.Id)
		repos = append(repos, repo)
	}

	client := pb.NewRepoServiceClient(suite.grpcClient)
	upload := suite.uploadPic(suite.users.Krosh.Identity)
	_, err := client.UpdateImage(ctx, &pb.UpdateRepoImageRequest{
		Id:        repos[1].Id,
		UploadKey: upload.Key,
		Image:     pb.UpdateRepoImageRequest_LOGO,
	})
	require.NoError(t, err)

	repos[1], err = client.Get(ctx, &pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: repos[1].Id}})
	require.NoError(t, err)

	testCases := []struct {
		name     string
		query    string
		caller   *entities.User
		expected []*pb.Repository
	}{
		{
			name:     "query by krosh",
			caller:   suite.users.Krosh,
			query:    "repo",
			expected: repos[:10],
		},
		{
			name:   "query by barash",
			caller: suite.users.Barash,
			query:  "repo",
			expected: functools.Filter(repos[:10], func(repo *pb.Repository) bool {
				return repo.Visibility == pb.ResourceVisibility_RESOURCE_PUBLIC
			}),
		},
		{
			name:     "not found",
			caller:   suite.users.Krosh,
			query:    "abc",
			expected: nil,
		},
	}

	c := pb.NewSearchServiceClient(suite.grpcClient)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callerCtx := testutils.AuthorizeGRPC(tc.caller.Identity)
			request := &pb.SuggestRepositoriesOrgRequest{
				Id:    org.Id,
				Query: tc.query,
			}
			resp, err := c.SuggestRepositoriesOrg(callerCtx, request)

			yarequire.ProtoStatusEqual(t, codes.OK, err)

			require.Equal(t, len(tc.expected), len(resp.Repositories))
			for i, repo := range resp.Repositories {
				require.Equal(t, tc.expected[i].Id, repo.Id)
				require.Equal(t, tc.expected[i].Slug, repo.Slug)
				require.Equal(t, tc.expected[i].Name, repo.Name)
				require.Equal(t, tc.expected[i].Description, repo.Description)
				require.Equal(t, tc.expected[i].OrgSlug, repo.OrgSlug)
				require.Equal(t, tc.expected[i].ProjectSlug, repo.ProjectSlug)
				if tc.expected[i].Logo != nil {
					require.NotNil(t, repo.Logo)
					require.Equal(t, tc.expected[i].Logo.Url, repo.Logo.Url)
				} else {
					require.Nil(t, repo.Logo)
				}
			}
		})
	}
}
