package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingMergeBases() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.MergeBasesRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.MergeBasesRequest{
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
			},
		},
		"invert": {
			request: &pb.MergeBasesRequest{
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
			},
		},
		"same rev": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
					},
				},
			},
		},
		"nil target": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
			code: codes.InvalidArgument,
		},
		"multiple merge bases": {
			request: &pb.MergeBasesRequest{
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
			},
		},
		"not_found_org": {
			request: &pb.MergeBasesRequest{
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
		"forbidden": {
			request: &pb.MergeBasesRequest{
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
		"happy_path_treediff": {
			request: &pb.MergeBasesRequest{
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
		"same_branch_treediff": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
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
		"ahead_treediff": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "rename",
					},
				},
			},
		},
		"behind_treediff": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "rename",
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
		"unrelated_treediff": {
			request: &pb.MergeBasesRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
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
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.MergeBases(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
