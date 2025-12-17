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

func (suite *RepoApiTestSuite) TestGrpcPlumpingTreeDiff() {
	t := suite.T()
	client := pb.NewDiffServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.TreeDiffRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.TreeDiffRequest{
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
			},
		},
		"revert": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
		},
		"tags": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v1.0",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v3-an",
					},
				},
			},
		},
		"default": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
		},
		"same": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
			},
		},
		"nil target": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
			},
		},
		"renames": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "a17aa945be4a0b0c14e0318fb3f45aacceb0c3b5",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
					},
				},
			},
		},
		"merge_base": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				UseMergeBase: true,
			},
		},
		"merge_base invert": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				UseMergeBase: true,
			},
		},
		"multiple merge bases": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "F",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "G",
					},
				},
				UseMergeBase: true,
			},
		},
		"not_found_org": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: plumbing.Hash{1}.String(),
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.TreeDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
					},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.TreeDiff(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
