package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingCommitGet() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.GetCommitRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.GetCommitRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"commit": {
			request: &pb.GetCommitRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
					},
				},
			},
		},
		"branch": {
			request: &pb.GetCommitRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
			},
		},
		"not_found_org": {
			request: &pb.GetCommitRequest{
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
			request: &pb.GetCommitRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found_rev",
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.GetCommitRequest{
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
			resp, err := client.Get(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
