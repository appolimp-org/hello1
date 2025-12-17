package integrationtests

import (
	"common/testutils/yarequire"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcRepoNormalizeSlug() {
	t := suite.T()
	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	tt := map[string]struct {
		request *pb.NormalizeSlugRequest
		code    codes.Code
	}{
		"happy path": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "Мой мега репозиторий",
			},
		},
		"invalid slug": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "repos",
			},
		},
		"slug collision": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "alPHa",
			},
		},
		"chinese": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "你好",
			},
		},
		"unicode": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "☃ Happy new year! ☃",
			},
		},
		"trimmed completely": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "!!!!!!!!!!!!",
			},
		},
		"special slug .sourcecraft": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: ".sourcecraft",
			},
		},
		"special slug .SourceCraft uppercase": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: ".SourceCraft",
			},
		},
		"slug with slashes": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "some/path",
			},
		},
		"slug with backslashes": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "some\\path",
			},
		},
		"slug with dots becomes hyphens": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: "Yandex.Cloud",
			},
		},
		"single dot forbidden": {
			request: &pb.NormalizeSlugRequest{
				OrgId:     grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				DirtySlug: ".",
			},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			resp, err := client.NormalizeSlug(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.code, err)
			if tc.code != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
