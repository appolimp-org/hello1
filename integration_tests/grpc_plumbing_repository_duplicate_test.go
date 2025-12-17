package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingRepositoryDuplicate() {
	t := suite.T()
	client := pb.NewRepositoryServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	successRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	failRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)

	tt := map[string]struct {
		request *pb.DuplicateRepositoryRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.DuplicateRepositoryRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(successRepoID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Reference: "refs/heads/master",
				SourceRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"not_found_repo": {
			request: &pb.DuplicateRepositoryRequest{
				Revision: &pb.GitRevision{
					RepoId: "9999",
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				SourceRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Reference: "refs/heads/master",
			},
			code: codes.NotFound,
		},
		"not_found_source_repo": {
			request: &pb.DuplicateRepositoryRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(failRepoID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				SourceRevision: &pb.GitRevision{
					RepoId: "999999",
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Reference: "refs/heads/master",
			},
			code: codes.NotFound,
		},
		"not_found_source_rev": {
			request: &pb.DuplicateRepositoryRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(failRepoID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				SourceRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not_found_rev",
					},
				},
				Reference: "refs/heads/master",
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.DuplicateRepositoryRequest{
				Revision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(failRepoID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Reference: "refs/heads/master",
				SourceRevision: &pb.GitRevision{
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
			resp, err := client.DuplicateRepository(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
