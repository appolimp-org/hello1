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

func (suite *RepoApiTestSuite) TestGrpcPlumpingReferenceUpdate() {
	t := suite.T()
	client := pb.NewReferenceServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.BranchPolicy, iam.Roles.RepositoriesMaintainer)

	tt := map[string]struct {
		request *pb.UpdateReferenceRequest
		code    codes.Code
	}{
		"branch_create": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/new-branch",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"branch_already_exists": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/master",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.AlreadyExists,
		},
		"branch_create_policy": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/releases/policy",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"branch_invalid_name": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/invalid/branch?name",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.InvalidArgument,
		},
		"branch_revision_not_found": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/invalid/branch?name",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not-found",
					},
				},
			},
			code: codes.NotFound,
		},
		"branch_delete": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/branch",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
			},
		},
		"branch_already_deleted": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/not-found",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
			},
			code: codes.NotFound,
		},
		"branch_delete_policy": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/legacy/feature1",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "legacy/feature1",
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"branch_delete_default": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/master",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"tag_create": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/tags/1.0",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
		},
		"tag_already_exists": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/tags/gitcore-v1.0.0",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.AlreadyExists,
		},
		"tag_invalid_name": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/tags/invalid/tag?name",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.InvalidArgument,
		},
		"tag_revision_not_found": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/invalid/branch?name",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "not-found",
					},
				},
			},
			code: codes.NotFound,
		},
		"tag_delete": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/tags/file-v1",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Tag{
						Tag: "file-v1",
					},
				},
			},
		},
		"repo_not_found": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/new-branch",
				NewRevision: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_DefaultBranch{
						DefaultBranch: true,
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/new-branch",
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
			},
			code: codes.PermissionDenied,
		},
		"branch_fast_forward": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/forupdate1",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "forupdate1",
					},
				},
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "7f1abf049f482ff11683106f69e728a883a02cf1",
					},
				},
			},
		},
		"branch_conflict": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/forupdate2",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "36bc7e4edeaa6c65aa617ce05ad28c3c207f5116",
					},
				},
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "7f1abf049f482ff11683106f69e728a883a02cf1",
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"branch_update_unknown_hash": {
			request: &pb.UpdateReferenceRequest{
				Reference: "refs/heads/forupdate2",
				OldRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "forupdate2",
					},
				},
				NewRevision: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "aabbccddeeff9b2a7875414a4f803ccc2ee57193",
					},
				},
			},
			code: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.Update(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
