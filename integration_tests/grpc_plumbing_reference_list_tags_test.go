package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingReferenceListTags() {
	t := suite.T()
	client := pb.NewReferenceServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.BranchPolicy, iam.Roles.RepositoriesMaintainer)

	tt := map[string]struct {
		request *pb.ListTagsRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.ListTagsRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
			},
		},
		"limit": {
			request: &pb.ListTagsRequest{
				RepoId:   grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				PageSize: 1,
				SortBy: []*pagination.SortOption{
					{
						Column:    "name",
						Direction: pagination.SortOption_DESC,
					},
				},
			},
		},
		"sort_by_name": {
			request: &pb.ListTagsRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination.SortOption{
					{
						Column:    "name",
						Direction: pagination.SortOption_ASC,
					},
				},
			},
		},
		"sort_by_commit": {
			request: &pb.ListTagsRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination.SortOption{
					{
						Column:    "committer_date",
						Direction: pagination.SortOption_DESC,
					},
				},
			},
		},
		"sort_both": {
			request: &pb.ListTagsRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				SortBy: []*pagination.SortOption{
					{
						Column:    "name",
						Direction: pagination.SortOption_DESC,
					},
					{
						Column:    "committer_date",
						Direction: pagination.SortOption_DESC,
					},
				},
			},
		},
		"filter": {
			request: &pb.ListTagsRequest{
				RepoId:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				NameContains: "v2",
			},
		},
		"filter_empty": {
			request: &pb.ListTagsRequest{
				RepoId:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				NameContains: "<nothing>",
			},
		},
		"filter_escaping": {
			request: &pb.ListTagsRequest{
				RepoId:       grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				NameContains: "%25v2",
			},
		},
		"repo_not_found": {
			request: &pb.ListTagsRequest{
				RepoId: "99999",
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.ListTagsRequest{
				RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
			},
			code: codes.PermissionDenied,
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

	t.Run("pagination", func(t *testing.T) {
		var prevPageToken, nextPageToken string

		t.Run("first", func(t *testing.T) {
			resp, err := client.ListTags(ctx, &pb.ListTagsRequest{
				RepoId:   grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				PageSize: 6,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)

			nextPageToken = resp.NextPageToken
		})

		t.Run("next", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := client.ListTags(ctx, &pb.ListTagsRequest{
				RepoId:    grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				PageSize:  1,
				PageToken: nextPageToken,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)

			prevPageToken = resp.PrevPageToken
		})

		t.Run("prev", func(t *testing.T) {
			require.NotEmpty(t, prevPageToken)

			resp, err := client.ListTags(ctx, &pb.ListTagsRequest{
				RepoId:    grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
				PageSize:  2,
				PageToken: prevPageToken,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	})
}
