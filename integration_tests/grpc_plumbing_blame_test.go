package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingBlame() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.BlameRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Blame.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: "file3.txt",
				Options: &pb.BlameRequest_Options{
					LineStart: utils.PtrFromValue(int32(2)),
					LineEnd:   utils.PtrFromValue(int32(8)),
				},
			},
		},
		"rename": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Blame.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "256b095a4cee4a9e19da2e736586d92f6c6224b1",
					},
				},
				Path: "file-rename.txt",
			},
		},
		"invalid_range": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Blame.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: "file3.txt",
				Options: &pb.BlameRequest_Options{
					LineStart: utils.PtrFromValue(int32(100)),
				},
			},
			code: codes.InvalidArgument,
		},
		"not_found_org": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: "999999",
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: "README.md",
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Blame.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found",
					},
				},
				Path: "README.md",
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.BlameRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: "README.md",
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.Blame(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
