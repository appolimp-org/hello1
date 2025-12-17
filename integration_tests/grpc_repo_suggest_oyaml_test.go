package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcRepo_SuggestSrcYamlTemplate() {
	t := suite.T()

	templateSlugs := []string{"Go", "Python", "Hello-World"}

	for _, slug := range templateSlugs {
		err := suite.SrcYamlTemplatesRepository.Create(context.Background(), &entities.SrcYamlTemplate{
			Slug:    slug,
			Content: "content",
		})
		require.NoError(t, err)
	}

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	tests := []struct {
		name         string
		query        string
		wantResult   []string
		expectedCode codes.Code
	}{
		{
			name:         "ok G",
			query:        "G",
			wantResult:   []string{"Go"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok Python",
			query:        "Python",
			wantResult:   []string{"Python"},
			expectedCode: codes.OK,
		},
		{
			name:         "non existing",
			query:        "NonExisting",
			wantResult:   nil,
			expectedCode: codes.OK,
		},
		{
			name:         "empty string",
			query:        "",
			wantResult:   []string{"Go", "Hello-World", "Python"},
			expectedCode: codes.OK,
		},
		{
			name:         "too long string",
			query:        strings.Repeat("a", 300),
			wantResult:   nil,
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := client.SuggestSrcYamlTemplate(ctx, &pb.SuggestSrcYamlTemplateRequest{
				Query: test.query,
			})
			if test.expectedCode != codes.OK {
				yarequire.ProtoStatusEqual(t, test.expectedCode, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, test.wantResult, resp.Slugs)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcRepo_ListSrcYamlTemplates() {
	t := suite.T()

	templateSlugs := []string{"Go", "Python", "Hello-World"}

	for _, slug := range templateSlugs {
		err := suite.SrcYamlTemplatesRepository.Create(context.Background(), &entities.SrcYamlTemplate{
			Slug:    slug,
			Content: "content",
		})
		require.NoError(t, err)
	}

	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	tests := []struct {
		name         string
		filter       *string
		wantResult   []string
		expectedCode codes.Code
	}{
		{
			name:         "ok G",
			filter:       utils.PtrFromValue("G"),
			wantResult:   []string{"Go"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok Python",
			filter:       utils.PtrFromValue("Python"),
			wantResult:   []string{"Python"},
			expectedCode: codes.OK,
		},
		{
			name:         "non existing",
			filter:       utils.PtrFromValue("NonExisting"),
			wantResult:   []string{},
			expectedCode: codes.OK,
		},
		{
			name:         "empty filter",
			filter:       utils.PtrFromValue(""),
			wantResult:   []string{"Go", "Hello-World", "Python"},
			expectedCode: codes.OK,
		},
		{
			name:         "nil filter",
			filter:       nil,
			wantResult:   []string{"Go", "Hello-World", "Python"},
			expectedCode: codes.OK,
		},
		{
			name:         "too long string",
			filter:       utils.PtrFromValue(strings.Repeat("a", 300)),
			wantResult:   nil,
			expectedCode: codes.InvalidArgument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := []string{}
			var pageToken *string
			for {
				resp, err := client.ListSrcYamlTemplates(ctx, &pb.ListSrcYamlTemplatesRequest{
					Filter:    test.filter,
					PageSize:  utils.PtrFromValue(uint64(2)),
					PageToken: pageToken,
				})
				if test.expectedCode != codes.OK {
					yarequire.ProtoStatusEqual(t, test.expectedCode, err)
					return
				}
				require.NoError(t, err)
				res = append(res, resp.Slugs...)
				if resp.NextPageToken == "" {
					break
				}
				pageToken = &resp.NextPageToken
			}
			require.EqualValues(t, test.wantResult, res)
		})
	}
}
