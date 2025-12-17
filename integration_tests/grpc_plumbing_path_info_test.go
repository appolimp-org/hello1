package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingPathInfo() {
	t := suite.T()
	client := pb.NewContentServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.PathInfoRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"file": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: ".gitignore",
			},
		},
		"dir rev": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "35e85108805c84807bc66a02d91535e1e24b38b9",
					},
				},
			},
		},

		"dir tag": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v2.b1",
					},
				},
				Path: "file1.txt",
			},
		},
		"annotated dir tag": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v2.main-an",
					},
				},
				Path: "file1.txt",
			},
		},
		"empty folder": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTree.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "xxx/yyy",
			},
		},

		"no such path": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "nada/sucho/patho",
			},
			code: codes.NotFound,
		},

		"no such rev": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "no-tag",
					},
				},
			},
			code: codes.NotFound,
		},

		"copy objects": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.PathInfo.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Path: "foo",
			},
		},

		"submodule": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.SubmoduleParent.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
		},

		"submodule deep": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.SubmoduleParent.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Path: "deep",
			},
		},
		"symlinks": {
			request: &pb.PathInfoRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.SymLink.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
		},
		"forbidden": {
			request: &pb.PathInfoRequest{
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
			resp, err := client.PathInfo(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
