package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingReferenceResolve() {
	t := suite.T()
	client := pb.NewReferenceServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.BranchPolicy, iam.Roles.RepositoriesMaintainer)

	tt := map[string]struct {
		request *pb.ResolveReferenceRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"commit": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "39427c55a3c59b91b11b5c1413033ebb85f84364",
					},
				},
			},
		},
		"branch": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
		},
		"tag": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v2.main",
					},
				},
			},
		},
		"annotated_tag": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.ListTags.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "v2.main-an",
					},
				},
			},
		},
		"without_refs": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Blame.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "43683522b7adcfb445b2c9323083c8a2f9cde4b8",
					},
				},
			},
		},
		"merge": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "1669dce138d9b841a518c64b10914d88f5e488ea",
					},
				},
			},
		},
		"not_found_rev": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found_rev",
					},
				},
			},
			code: codes.NotFound,
		},
		"repo_not_found": {
			request: &pb.ResolveReferenceRequest{
				Revision: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.ResolveReferenceRequest{
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
			resp, err := client.Resolve(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
