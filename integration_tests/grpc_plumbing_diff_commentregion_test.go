package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingCommentRegion() {
	t := suite.T()
	client := pb.NewDiffServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.CommentRegionRequest
		code    codes.Code
	}{
		"modified": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file1.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
		},
		"deleted": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file3.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
		},
		"added": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file4.txt",
				Side:     pb.CommentRegionRequest_SIDE_SOURCE,
				FromLine: 1,
				ToLine:   1,
			},
		},
		"deleted_in_source": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file3.txt",
				Side:     pb.CommentRegionRequest_SIDE_SOURCE,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.NotFound,
		},
		"deleted_in_target": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file4.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.NotFound,
		},
		"directory": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir5",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.FailedPrecondition,
		},
		"not_found_org": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: "9999",
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: "9999",
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file1.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.NotFound,
		},
		"not_found_branch": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found_branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file1.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: plumbing.Hash{1}.String(),
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file1.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.CommentRegionRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Path:     "dir3/file1.txt",
				Side:     pb.CommentRegionRequest_SIDE_TARGET,
				FromLine: 1,
				ToLine:   1,
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.CommentRegion(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
