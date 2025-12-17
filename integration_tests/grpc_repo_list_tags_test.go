package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"path"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) createTag(user *entities.User, repo *entities.Repository, refName plumbing.ReferenceName, tag string) {
	t := suite.T()

	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "")
	repoURL := protocol.RepoURL(repo.RepoFullSlug().OrgSlug, repo.RepoFullSlug().RepoSlug)

	cg := protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), user.Identity).WithAuthTokenSite(repoURL)
	cg.Must(t, "clone", repoURL, "repo")
	repoDir := testutils.NewWorkdir(t, path.Join(tmpDir, "repo"))
	cg = protocol.PrepareCGit(repoDir, user.Identity).WithAuthTokenSite(repoURL)

	cg.Must(t, "tag", tag, refName.Short())
	cg.Must(t, "push", "origin", "--tags")
}

func (suite *RwApiTestSuite) TestGrpcListTags() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.ListTagsRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
		},
		"pagination-0": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
				PageSize: utils.PtrFromValue(uint64(3)),
			},
		},
		"pagination-1": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
				PageSize:  utils.PtrFromValue(uint64(3)),
				PageToken: utils.PtrFromValue("g6dDb2x1bW5zkoOkTmFtZaRuYW1lqURpcmVjdGlvbsKlVmFsdWWucmVmcy90YWdzL3YxLjCDpE5hbWWhX6lEaXJlY3Rpb27CpVZhbHVlxBQ5QnxVo8WbkbEbXBQTAz67hfhDZKlEaXJlY3Rpb27CqkNsb3VkVG9rZW7A"),
			},
		},
		"pagination-2": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
				PageSize:  utils.PtrFromValue(uint64(3)),
				PageToken: utils.PtrFromValue("g6dDb2x1bW5zkoOkTmFtZaRuYW1lqURpcmVjdGlvbsKlVmFsdWW0cmVmcy90YWdzL3YyLm1haW4tYW6DpE5hbWWhX6lEaXJlY3Rpb27CpVZhbHVlxBQTmWMucomQzhLDcDoxNO1KfmJzgqlEaXJlY3Rpb27CqkNsb3VkVG9rZW7A"),
			},
		},
		"committer date desc": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "committer_date",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
			},
		},
		"sort both": {
			request: &pb.ListTagsRequest{
				Id: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_DESC,
					},
					{
						Column:    "committer_date",
						Direction: pagination_pb.SortOption_DESC,
					},
				},
			},
		},
		"filter": {
			request: &pb.ListTagsRequest{
				Id:     grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				Filter: utils.PtrFromValue("v2"),
				SortBy: []*pagination_pb.SortOption{
					{
						Column:    "name",
						Direction: pagination_pb.SortOption_ASC,
					},
				},
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.ListTags(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
