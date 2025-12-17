package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGrpcLicensePreset_Create() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewLicensePresetsServiceClient(suite.grpcClient)

	tests := []struct {
		name         string
		preset       *pb.CreateLisencePresetRequest
		expectedCode codes.Code
	}{
		{
			name: "ok without priority",
			preset: &pb.CreateLisencePresetRequest{
				Slug:    "GPL-3.0-or-later",
				License: "Some content",
			},
			expectedCode: codes.OK,
		},
		{
			name: "ok with priority",
			preset: &pb.CreateLisencePresetRequest{
				Slug:     "MIT",
				License:  "Some content",
				Priority: utils.PtrFromValue(uint32(1)),
			},
			expectedCode: codes.OK,
		},
		{
			name: "empty slug",
			preset: &pb.CreateLisencePresetRequest{
				Slug:    "",
				License: "Some content",
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "existing slug",
			preset: &pb.CreateLisencePresetRequest{
				Slug:    "GPL-3.0-or-later",
				License: "Some content",
			},
			expectedCode: codes.AlreadyExists,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.Create(ctx, test.preset)
			if test.expectedCode != codes.OK {
				yarequire.ProtoStatusEqual(t, test.expectedCode, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcLicensePresets_List() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewLicensePresetsServiceClient(suite.grpcClient)

	presets := []*pb.CreateLisencePresetRequest{
		{
			Slug:     "GPL-3.0-or-later",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(1)),
		},
		{
			Slug:    "GLWTPL",
			License: "Some content",
		},
		{
			Slug:     "MIT",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(2)),
		},
		{
			Slug:     "LGPL-2.0-only",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(1)),
		},
	}

	for _, preset := range presets {
		_, err := client.Create(ctx, preset)
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
			name:         "ok G",
			filter:       utils.PtrFromValue("G"),
			wantResult:   []string{"GPL-3.0-or-later", "LGPL-2.0-only", "GLWTPL"},
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
			wantResult:   []string{"MIT", "GPL-3.0-or-later", "LGPL-2.0-only", "GLWTPL"},
			expectedCode: codes.OK,
		},
		{
			name:         "nil filter",
			filter:       utils.PtrFromValue(""),
			wantResult:   []string{"MIT", "GPL-3.0-or-later", "LGPL-2.0-only", "GLWTPL"},
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
				resp, err := client.List(ctx, &pb.ListLicensePresetsRequest{
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

func (suite *RwApiTestSuite) TestGrpcLicensePresets_Suggest() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Internal.Identity)
	client := pb.NewLicensePresetsServiceClient(suite.grpcClient)

	presets := []*pb.CreateLisencePresetRequest{
		{
			Slug:     "GPL-3.0-or-later",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(1)),
		},
		{
			Slug:    "GLWTPL",
			License: "Some content",
		},
		{
			Slug:     "MIT",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(2)),
		},
		{
			Slug:     "LGPL-2.0-only",
			License:  "Some content",
			Priority: utils.PtrFromValue(uint32(1)),
		},
	}

	for _, preset := range presets {
		_, err := client.Create(ctx, preset)
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
			name:         "ok G",
			query:        "G",
			wantResult:   []string{"GPL-3.0-or-later", "LGPL-2.0-only", "GLWTPL"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok GL",
			query:        "GL",
			wantResult:   []string{"GLWTPL"},
			expectedCode: codes.OK,
		},
		{
			name:         "ok t",
			query:        "t",
			wantResult:   []string{"MIT", "GPL-3.0-or-later", "GLWTPL"},
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
			wantResult:   []string{"MIT", "GPL-3.0-or-later", "LGPL-2.0-only", "GLWTPL"},
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
			resp, err := client.Suggest(ctx, &pb.SuggestLicensePresetsRequest{
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
