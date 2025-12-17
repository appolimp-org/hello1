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

func (suite *RepoApiTestSuite) TestGrpcPlumpingCommitList() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.ListCommitsRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"since_rev": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
					},
				},
			},
		},
		// git log  --pretty=format:"%h" -- LICENSE
		"file": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
					},
				},
				Path: utils.PtrFromValue("LICENSE"),
			},
		},
		// git log  --pretty=format:"%h" -- CHANGELOG
		"file2": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: utils.PtrFromValue("CHANGELOG"),
			},
		},
		// git log  --pretty=format:"%h" -- go
		"dir": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Path: utils.PtrFromValue("go"),
			},
		},
		"threedot": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ThreeDot.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				ToRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ThreeDot.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
		},
		// git log --pretty=format:"%h" -- 1
		"merge_history": {
			request: &pb.ListCommitsRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.MergeHistory.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Path: utils.PtrFromValue("1"),
			},
		},
		"not_found_org": {
			request: &pb.ListCommitsRequest{
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
			request: &pb.ListCommitsRequest{
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
			request: &pb.ListCommitsRequest{
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
			resp, err := client.List(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
