package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
)

func (suite *RwApiTestSuite) TestGrpcEvaluatePathFilters() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tt := map[string]struct {
		request *pb.EvaluatePathFiltersRequest
		code    codes.Code
	}{
		"happy_path": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "main",
				SourceRev: "branch",
				PathFilters: []*pb.PathFilter{
					// specific_files
					{Patterns: []string{"file1.txt"}},
					{Patterns: []string{"file4.txt"}},
					// negation
					{Patterns: []string{"dir*/**", "!dir1/**", "!dir5/**"}},
					// multiple_patterns
					{Patterns: []string{"dir1/**", "dir2/**"}},
					{Patterns: []string{"dir3/**", "dir5/**"}},
					// double_star
					{Patterns: []string{"**/file1.txt"}},
					// complex
					{Patterns: []string{"**/*.txt", "!**/file3.txt"}},
					// no_match
					{Patterns: []string{"**/*.md"}},
					{Patterns: []string{"dir3/**", "!dir3/**"}},
					{Patterns: []string{"file_nonexistent.txt"}},
					{Patterns: []string{"**/test_*.txt", "docs/**/*.md"}},
				},
			},
		},
		"renames": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "a17aa945be4a0b0c14e0318fb3f45aacceb0c3b5",
				SourceRev: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{"renamed_dir/**"}},
					{Patterns: []string{".gitmodules"}},
					{Patterns: []string{"subdir/**"}},
					{Patterns: []string{"renamed_dir/**", "!renamed_dir/inner/**", "!renamed_dir/inner2/**"}},
				},
			},
		},
		"first-commit": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "0000000000000000000000000000000000000000",
				SourceRev: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{"renamed_dir/**"}},
					{Patterns: []string{".gitmodules"}},
				},
			},
		},
		"removing-branch": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "575b2e9c3c8d3b433260b84328e4f884bcaf7721",
				SourceRev: "0000000000000000000000000000000000000000",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{"renamed_dir/**"}},
					{Patterns: []string{".gitmodules"}},
				},
			},
			code: codes.InvalidArgument,
		},
		"empty_changes": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "9992a6f69d20dd07d47649125b0d2e9092b4c342",
				SourceRev: "9992a6f69d20dd07d47649125b0d2e9092b4c342",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{"dir1/**"}},
				},
			},
		},
		"not_found_revision": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "xxxx",
				SourceRev: "main",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{""}},
				},
			},
			code: codes.NotFound,
		},
		"validation_PathFilters": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "xxxx",
				SourceRev: "main",
			},
			code: codes.InvalidArgument,
		},
		"validation_Patterns": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.TreeDiff.ID),
				TargetRev: "xxxx",
				SourceRev: "main",
				PathFilters: []*pb.PathFilter{
					{},
					{},
				},
			},
			code: codes.InvalidArgument,
		},
		"forbidden": {
			request: &pb.EvaluatePathFiltersRequest{
				Id:        grpc_marshalling.IDInverse(suite.repos.AuthRepoInternal.ID),
				TargetRev: "main",
				SourceRev: "branch",
				PathFilters: []*pb.PathFilter{
					{Patterns: []string{"*"}},
				},
			},
			code: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.EvaluatePathFilters(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
