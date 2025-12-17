package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingCommitCreate() {
	t := suite.T()
	client := pb.NewCommitServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	suite.addRole(t, suite.users.Kopatych, suite.repos.BranchPolicy, iam.Roles.RepositoriesDeveloper)

	newRootBranch := &pb.GitRevision{
		RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
		Revision: &pb.GitRevision_DefaultBranch{
			DefaultBranch: true,
		},
	}
	nonPrBranch := &pb.GitRevision{
		RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
		Revision: &pb.GitRevision_Branch{
			Branch: "nonpr",
		},
	}
	notFoundOrg := &pb.GitRevision{
		RepoId: "99999",
		Revision: &pb.GitRevision_Branch{
			Branch: "main",
		},
	}
	notFoundRef := &pb.GitRevision{
		RepoId: grpc_marshalling.IDInverse(suite.repos.BranchPolicy.ID),
		Revision: &pb.GitRevision_Branch{
			Branch: "not_found_rev",
		},
	}
	mainRef := &pb.GitRevision{
		RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
		Revision: &pb.GitRevision_Branch{
			Branch: "main",
		},
	}

	tt := map[string]struct {
		request *pb.CreateCommitRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.CreateCommitRequest{
				Source:    newRootBranch,
				Parents:   []*pb.GitRevision{newRootBranch},
				Reference: "refs/heads/added",
				Message:   "TSK-084[internal]: Add new file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "tmp/file.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
		},
		"remove_file": {
			request: &pb.CreateCommitRequest{
				Source:    newRootBranch,
				Parents:   []*pb.GitRevision{newRootBranch},
				Reference: "refs/heads/removed",
				Message:   "TSK-084[internal]: delete file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						OldPath: "1.txt",
					},
				},
			},
		},
		"rename_file": {
			request: &pb.CreateCommitRequest{
				Source:    newRootBranch,
				Parents:   []*pb.GitRevision{newRootBranch},
				Reference: "refs/heads/renamed",
				Message:   "TSK-084[internal]: rename file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						OldPath: "1.txt",
						NewPath: "some/dir/1.txt",
					},
				},
			},
		},
		"modify_file": {
			request: &pb.CreateCommitRequest{
				Source:    newRootBranch,
				Parents:   []*pb.GitRevision{newRootBranch},
				Reference: "refs/heads/modified",
				Message:   "TSK-084[internal]: rename file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "1.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("new file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
		},
		"branch_policy": {
			request: &pb.CreateCommitRequest{
				Source:    nonPrBranch,
				Parents:   []*pb.GitRevision{nonPrBranch},
				Reference: "refs/heads/nonpr",
				Message:   "branch policy prevent nonpr changes",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "tmp/file.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
			code: codes.FailedPrecondition,
		},
		"not_found_org": {
			request: &pb.CreateCommitRequest{
				Source:    notFoundOrg,
				Parents:   []*pb.GitRevision{notFoundOrg},
				Reference: "refs/heads/something",
				Message:   "TSK-084[internal]: Add new file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "tmp/file.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
			code: codes.NotFound,
		},
		"not_found_rev": {
			request: &pb.CreateCommitRequest{
				Source:    notFoundRef,
				Parents:   []*pb.GitRevision{notFoundRef},
				Reference: "refs/heads/something",
				Message:   "TSK-084[internal]: Add new file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "tmp/file.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.CreateCommitRequest{
				Source:    mainRef,
				Parents:   []*pb.GitRevision{mainRef},
				Reference: "refs/heads/something",
				Message:   "TSK-084[internal]: Add new file",
				Actions: []*pb.CreateCommitRequest_CommitAction{
					{
						NewPath: "tmp/file.txt",
						FileContent: &pb.CreateCommitRequest_FileContent{
							ContentType: &pb.CreateCommitRequest_FileContent_Bytes{
								Bytes: []byte("file content"),
							},
						},
						FileMode: utils.PtrFromValue(pb.EntryType_ENTRY_TYPE_FILE),
					},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp,
				protocmp.IgnoreFields(&pb.Commit{}, "hash"),
				protocmp.IgnoreFields(&pb.Commit_Signature{}, "date"),
			)
		})
	}
}
