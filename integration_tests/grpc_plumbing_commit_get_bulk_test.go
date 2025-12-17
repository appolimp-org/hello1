package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingCommitGetBulk() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.GetBulkCommitRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "918c48b83bd081e863dbe1b80f8998f058cd8294",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "1669dce138d9b841a518c64b10914d88f5e488ea",
						},
					},
				},
			},
		},
		"duplicate": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
				},
			},
		},
		"different_org": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
				},
			},
			code: codes.Unimplemented,
		},
		"not_commit_revision": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Branch{
							Branch: "master",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_DefaultBranch{
							DefaultBranch: true,
						},
					},
				},
			},
			code: codes.Unimplemented,
		},
		"not_found_org": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: "99999",
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
				},
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "1669dce138d9b841a518c64b10914d88f5e488ea",
						},
					},
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "00000008800004807bc66a02d91535e1e24b38b9",
						},
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.GetBulkCommitRequest{
				Revisions: []*pb.GitRevision{
					{
						RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
						Revision: &pb.GitRevision_Commit{
							Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
						},
					},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.GetBulk(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
