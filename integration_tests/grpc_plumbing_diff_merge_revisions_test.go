package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RepoApiTestSuite) TestGrpcPlumbingMergeRevisions() {
	t := suite.T()
	client := pb.NewDiffServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	// Import a repo with merge conflicts for testing
	merge02 := suite.ImportRepo(suite.orgs.Yandex, "merge-02", "generated/merge.02", nil)

	suite.addRole(t, suite.users.Kopatych, merge02, iam.Roles.RepositoriesMaintainer)

	tt := map[string]struct {
		request *pb.MergeRevisionsRequest
		code    codes.Code
	}{
		"happy path, no conflict": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "B",
					},
				},
			},
		},
		"simple conflict": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "B",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "A",
					},
				},
			},
		},
		"repo not found": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Source: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
			code: codes.PermissionDenied,
		},
		"target not branch": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"nothing to merge": {
			request: &pb.MergeRevisionsRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "B",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(merge02.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.FailedPrecondition,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.MergeRevisions(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp, protocmp.IgnoreFields(
				&pb.MergeRevisionsConflict{},
				"id",
			), protocmp.IgnoreFields(
				&pb.ConflictMessage{},
				"id",
			))
		})
	}
}
