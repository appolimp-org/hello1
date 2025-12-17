package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/plumbing/v1"
	"testing"

	"google.golang.org/grpc/codes"
)

func (suite *RepoApiTestSuite) TestGrpcPlumpingFilesDiff() {
	t := suite.T()
	client := pb.NewDiffServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.FilesDiffRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{
						SourcePath: "README",
						TargetPath: "README",
					},
					{
						SourcePath: "vendor/foo.go",
						TargetPath: "vendor/foo.go",
					},
					{
						SourcePath: "no/such/file.go",
						TargetPath: "no/such/file.go",
					},
					{
						SourcePath: "vendor",
						TargetPath: "vendor",
					},
					{
						SourcePath: "binary.jpg",
						TargetPath: "binary.jpg",
					},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 5,
				},
			},
		},
		"same rev": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "README"},
					{SourcePath: "dont/read/me"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 5,
				},
			},
		},
		"nil target": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "README"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 5,
				},
			},
		},
		"too big": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "feature",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "toobig", TargetPath: "toobig"},
					{SourcePath: "okay", TargetPath: "okay"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 0,
					MaxOps:        400,
				},
			},
		},
		"too big, no limit": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.BigDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "feature",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "toobig", TargetPath: "toobig"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 0,
					MaxOps:        0,
				},
			},
		},
		"renamed": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.DifferentDiffs.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.DifferentDiffs.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "file1.txt", TargetPath: "file1_moved.txt"},
					{SourcePath: "file1.txt", TargetPath: "file3.txt"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 0,
				},
			},
		},
		"merge_base": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				UseMergeBase: true,
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "README", TargetPath: "README"},
				},
			},
		},
		"merge_base invert": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "master",
					},
				},
				UseMergeBase: true,
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "vendor/foo.go"},
				},
			},
		},
		"multiple merge bases": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "F",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.Crisscross.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "G",
					},
				},
				UseMergeBase: true,
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "a1", TargetPath: "a1"},
				},
				Options: &pb.FilesDiffRequest_Options{
					ContextLength: 0,
				},
			},
		},
		"utf abuse": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.UtfAbuse.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.UtfAbuse.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "utfabuse.txt", TargetPath: "utfabuse.txt"},
				},
			},
		},
		"no_paths": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "branch",
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
					Revision: &pb.GitRevision_Branch{
						Branch: "main",
					},
				},
				DiffPaths: []*pb.DiffPath{}, // empty
			},
			code: codes.InvalidArgument,
		},
		"not_found_org": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: "99999",
					Revision: &pb.GitRevision_Commit{
						Commit: "bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "README"},
				},
			},
			code: codes.NotFound,
		},
		"forbidden": {
			request: &pb.FilesDiffRequest{
				Target: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_EmptyRoot{
						EmptyRoot: true,
					},
				},
				Source: &pb.GitRevision{
					RepoId: grpc_marshalling.IDInverse(suite.repos.AuthRepoPrivate.ID),
					Revision: &pb.GitRevision_Commit{
						Commit: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
					},
				},
				DiffPaths: []*pb.DiffPath{
					{SourcePath: "README"},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.FilesDiff(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
