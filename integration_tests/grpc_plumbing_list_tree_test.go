package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingListTree() {
	t := suite.T()
	client := pb.NewContentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.ListTreeRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Recursive: true,
			},
		},
		"non_recursive": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"non_recursive_with_path": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				PathFilter: "foo/bar",
			},
		},
		"non_recursive_with_path_name": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				PathFilter: "foo/bar/vendor",
				NameFilter: "gog",
			},
		},
		"page_size": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Recursive: true,
				PageSize:  3,
			},
		},
		"page_size_big": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Recursive: true,
				PageSize:  100,
			},
		},
		"name_filter": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				NameFilter: "json",
				Recursive:  true,
			},
		},
		"name_filter_with_page_size": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				NameFilter: "json",
				Recursive:  true,
				PageSize:   2,
			},
		},
		"name_filter_with_case_insensitive": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				NameFilter:      "json",
				CaseInsensitive: true,
				Recursive:       true,
			},
		},
		"path_filter": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				PathFilter: "json/short.json",
				Recursive:  true,
			},
		},
		"path_filter_with_page_size": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				PathFilter: "json/short.json",
				Recursive:  true,
				PageSize:   1,
			},
		},
		"path_filter_dir": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				PathFilter: "json",
				Recursive:  true,
			},
		},
		"not_found_org": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found_rev",
					},
				},
			},
			code: codes.NotFound,
		},
		"revision_with_nulls": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch\x00with\x00nulls",
					},
				},
			},
			code: codes.InvalidArgument,
		},
		"forbidden": {
			request: &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.ListTree(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}

	t.Run("pagination", func(t *testing.T) {
		var nextPageToken string

		t.Run("first", func(t *testing.T) {
			resp, err := client.ListTree(ctx, &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				NameFilter: "a",
				Recursive:  true,
				PageSize:   2,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)

			nextPageToken = resp.NextPageToken
		})

		t.Run("next", func(t *testing.T) {
			require.NotEmpty(t, nextPageToken)

			resp, err := client.ListTree(ctx, &pb.ListTreeRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "any",
					},
				},
				NameFilter: "any",
				Recursive:  false,
				PageSize:   1,
				PageToken:  nextPageToken, // get all params from token
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	})

	t.Run("pagination_same", func(t *testing.T) {
		var treeEntries []*pb.TreeEntry

		revision := &pb.GitRevision{
			RepoId: grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
			Revision: &pb.GitRevision_DefaultBranch{
				DefaultBranch: true,
			},
		}

		resp, err := client.ListTree(ctx, &pb.ListTreeRequest{
			Revision:  revision,
			Recursive: true,
			PageSize:  3,
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)
		treeEntries = append(treeEntries, resp.Entries...)

		require.NotEmpty(t, resp.NextPageToken)
		for resp.NextPageToken != "" {
			resp, err = client.ListTree(ctx, &pb.ListTreeRequest{
				Revision:  revision,
				PageToken: resp.NextPageToken,
			})
			yarequire.ProtoStatusEqual(t, codes.OK, err)
			treeEntries = append(treeEntries, resp.Entries...)
		}

		respAll, err := client.ListTree(ctx, &pb.ListTreeRequest{
			Revision:  revision,
			Recursive: true,
		})
		yarequire.ProtoStatusEqual(t, codes.OK, err)

		yarequire.ProtoEqualList(t, treeEntries, respAll.Entries)
	})
}
