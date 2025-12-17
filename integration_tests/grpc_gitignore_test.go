package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcGitignorePresets_Create() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewGitignorePresetsServiceClient(suite.grpcClient)

	_, err := client.Create(ctx, &pb.CreateGitignorePresetRequest{
		Name:    "name",
		Content: "content",
	})
	require.NoError(t, err)
}

func (suite *RwApiTestSuite) TestGrpcGitignorePresets_Suggest() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewGitignorePresetsServiceClient(suite.grpcClient)

	presetNames := []string{"1C-Bitrix", "1C", "AL", "Ada", "Go", "C", "C++", "CMake"}

	for _, name := range presetNames {
		_, err := client.Create(ctx, &pb.CreateGitignorePresetRequest{
			Name:    name,
			Content: "content",
		})
		require.NoError(t, err)
	}

	ctx = testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tests := []struct {
		name         string
		query        string
		wantResult   []string
		expectedCode codes.Code
	}{
		{
			name:         "ok 1C",
			query:        "1C",
			wantResult:   []string{"1C", "1C-Bitrix"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok C",
			query:        "C",
			wantResult:   []string{"1C", "1C-Bitrix", "C", "C++", "CMake"},
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
			wantResult:   []string{"1C", "1C-Bitrix", "AL", "Ada", "C", "C++", "CMake", "Go"},
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
			resp, err := client.Suggest(ctx, &pb.SuggestGitignorePresetsRequest{
				Query: test.query,
			})
			if test.expectedCode != codes.OK {
				yarequire.ProtoStatusEqual(t, test.expectedCode, err)
				return
			}
			require.NoError(t, err)
			slices.Sort(resp.Names)
			require.EqualValues(t, test.wantResult, resp.Names)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcGitignorePresets_List() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewGitignorePresetsServiceClient(suite.grpcClient)

	presetNames := []string{"1C-Bitrix", "1C", "AL", "Ada", "Go", "C", "C++", "CMake"}

	for _, name := range presetNames {
		_, err := client.Create(ctx, &pb.CreateGitignorePresetRequest{
			Name:    name,
			Content: "content",
		})
		require.NoError(t, err)
	}

	ctx = testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	tests := []struct {
		name         string
		filter       *string
		wantResult   []string
		expectedCode codes.Code
	}{
		{
			name:         "ok 1C",
			filter:       utils.PtrFromValue("1C"),
			wantResult:   []string{"1C", "1C-Bitrix"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok C",
			filter:       utils.PtrFromValue("C"),
			wantResult:   []string{"1C", "1C-Bitrix", "C", "C++", "CMake"},
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
			wantResult:   []string{"1C", "1C-Bitrix", "Ada", "AL", "C", "C++", "CMake", "Go"},
			expectedCode: codes.OK,
		},
		{
			name:         "nil filter",
			filter:       nil,
			wantResult:   []string{"1C", "1C-Bitrix", "Ada", "AL", "C", "C++", "CMake", "Go"},
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
				resp, err := client.List(ctx, &pb.ListGitignorePresetsRequest{
					Filter:    test.filter,
					PageSize:  utils.PtrFromValue(uint64(2)),
					PageToken: pageToken,
				})
				if test.expectedCode != codes.OK {
					yarequire.ProtoStatusEqual(t, test.expectedCode, err)
					return
				}
				require.NoError(t, err)
				res = append(res, resp.Names...)
				if resp.NextPageToken == "" {
					break
				}
				pageToken = &resp.NextPageToken
			}
			require.EqualValues(t, test.wantResult, res)
		})
	}
}
