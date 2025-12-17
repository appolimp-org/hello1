package integrationtests

import (
	"common/testutils/assertjson"
	"common/utils"
	"fmt"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func (suite *RwApiTestSuite) TestTreeDiff() {
	t := suite.T()

	tests := []struct {
		name             string
		body             schemas.TreeDiffRequest
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
		repoSlug         string
	}{
		{
			name:     "main",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				TargetRev: utils.PtrFromValue("main"),
				SourceRev: utils.PtrFromValue("branch"),
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"result": [
					{"change":"D","path":"dir1","type":"dir"},
					{"change":"D","path":"dir1/file1.txt","type":"file"},
					{"change":"D","path":"dir1/file2.txt","type":"file"},
					{"change":"D","path":"dir1/file3.txt","type":"file"},
					{"change":"A","path":"dir2","type":"dir"},
					{"change":"A","path":"dir2/file1.txt","type":"file"},
					{"change":"A","path":"dir2/file2.txt","type":"file"},
					{"change":"A","path":"dir2/file3.txt","type":"file"},
					{"change":"M","path":"dir3","type":"dir"},
					{"change":"M","path":"dir3/file1.txt","type":"file"},
					{"change":"D","path":"dir3/file3.txt","type":"file"},
					{"change":"A","path":"dir3/file4.txt","type":"file"},
					{"change":"M","path":"dir5","type":"dir"},
					{"change":"A","path":"dir5/dir_to_file","type":"file"},
					{"change":"D","path":"dir5/dir_to_file","type":"dir"},
					{"change":"D","path":"dir5/dir_to_file/file2.txt","type":"file"},
					{"change":"A","path":"dir5/file_to_dir","type":"dir"},
					{"change":"D","path":"dir5/file_to_dir","type":"file"},
					{"change":"A","path":"dir5/file_to_dir/file1.txt","type":"file"},
					{"change":"M","path":"file1.txt","type":"file"},
					{"change":"D","path":"file3.txt","type":"file"},
					{"change":"A","path":"file4.txt","type":"file"}
				]}`,
		},
		{
			name:     "revert",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				TargetRev: utils.PtrFromValue("branch"),
				SourceRev: utils.PtrFromValue("main"),
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"result": [
					{"change":"A","path":"dir1","type":"dir"},
					{"change":"A","path":"dir1/file1.txt","type":"file"},
					{"change":"A","path":"dir1/file2.txt","type":"file"},
					{"change":"A","path":"dir1/file3.txt","type":"file"},
					{"change":"D","path":"dir2","type":"dir"},
					{"change":"D","path":"dir2/file1.txt","type":"file"},
					{"change":"D","path":"dir2/file2.txt","type":"file"},
					{"change":"D","path":"dir2/file3.txt","type":"file"},
					{"change":"M","path":"dir3","type":"dir"},
					{"change":"M","path":"dir3/file1.txt","type":"file"},
					{"change":"A","path":"dir3/file3.txt","type":"file"},
					{"change":"D","path":"dir3/file4.txt","type":"file"},
					{"change":"M","path":"dir5","type":"dir"},
					{"change":"A","path":"dir5/dir_to_file","type":"dir"},
					{"change":"D","path":"dir5/dir_to_file","type":"file"},
					{"change":"A","path":"dir5/dir_to_file/file2.txt","type":"file"},
					{"change":"A","path":"dir5/file_to_dir","type":"file"},
					{"change":"D","path":"dir5/file_to_dir","type":"dir"},
					{"change":"D","path":"dir5/file_to_dir/file1.txt","type":"file"},
					{"change":"M","path":"file1.txt","type":"file"},
					{"change":"A","path":"file3.txt","type":"file"},
					{"change":"D","path":"file4.txt","type":"file"}
				]}`,
		},
		{
			name:     "not_found",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				TargetRev: utils.PtrFromValue("random"),
				SourceRev: utils.PtrFromValue("main"),
			},
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrNotFoundRevision.ErrorCode,
		},
		{
			name:     "empty",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				TargetRev: utils.PtrFromValue("9992a6f69d20dd07d47649125b0d2e9092b4c342"),
				SourceRev: utils.PtrFromValue("9992a6f69d20dd07d47649125b0d2e9092b4c342"),
			},
			expectedCode:     http.StatusOK,
			expectedResponse: `{"result": []}`,
		},
		{
			name:     "nil target",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				SourceRev: utils.PtrFromValue("bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6"),
				TargetRev: nil,
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
"result":	[{"change":"A","path":"dir1","type":"dir"},
	{"change":"A","path":"dir1/file1.txt","type":"file"},
	{"change":"A","path":"dir1/file2.txt","type":"file"},
	{"change":"A","path":"dir1/file3.txt","type":"file"},
	{"change":"A","path":"dir3","type":"dir"},
	{"change":"A","path":"dir3/file1.txt","type":"file"},
	{"change":"A","path":"dir3/file2.txt","type":"file"},
	{"change":"A","path":"dir3/file3.txt","type":"file"},
	{"change":"A","path":"dir4","type":"dir"},
	{"change":"A","path":"dir4/file1.txt","type":"file"},
	{"change":"A","path":"dir4/file2.txt","type":"file"},
	{"change":"A","path":"dir4/file3.txt","type":"file"},
	{"change":"A","path":"dir5","type":"dir"},
	{"change":"A","path":"dir5/dir_to_file","type":"dir"},
	{"change":"A","path":"dir5/dir_to_file/file2.txt","type":"file"},
	{"change":"A","path":"dir5/file_to_dir","type":"file"},
	{"change":"A","path":"file1.txt","type":"file"},
	{"change":"A","path":"file2.txt","type":"file"},
	{"change":"A","path":"file3.txt","type":"file"}
]}`,
		},
		{
			name:     "nil src",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				SourceRev: nil,
				TargetRev: utils.PtrFromValue("bd9c8dc8ffacb564f0c001d1c7357bf9b3cf05d6"),
			},
			expectedCode: http.StatusBadRequest,
			expectedResponse: `{
  "details": {
    "source": {
      "code": "validation_required",
      "message": "cannot be blank"
    }
  },
  "error_code": "validation_failed",
  "message": "Bad Request"
}`,
		},
		{
			name:     "both nil",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				SourceRev: nil,
				TargetRev: nil,
			},
			expectedCode: http.StatusBadRequest,
			expectedResponse: `{
  "details": {
    "source": {
      "code": "validation_required",
      "message": "cannot be blank"
    }
  },
  "error_code": "validation_failed",
  "message": "Bad Request"
}`,
		},
		{
			name:     "from base (src: branch, trg: master)",
			repoSlug: "yandex/alpha",
			body: schemas.TreeDiffRequest{
				TargetRev:          utils.PtrFromValue("master"),
				SourceRev:          utils.PtrFromValue("branch"),
				RelatedToMergeBase: true,
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"result": [
					{"change":"A","path":"README","type":"file"}
				]}`,
		},
		{
			name:     "from base (src: master, trg: branch)",
			repoSlug: "yandex/alpha",
			body: schemas.TreeDiffRequest{
				TargetRev:          utils.PtrFromValue("branch"),
				SourceRev:          utils.PtrFromValue("master"),
				RelatedToMergeBase: true,
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"result": [
					{"change":"A","path":"vendor","type":"dir"},
					{"change":"A","path":"vendor/foo.go","type":"file"}
				]}`,
		},
		{
			name:     "renames",
			repoSlug: "yandex/treediff",
			body: schemas.TreeDiffRequest{
				TargetRev:          utils.PtrFromValue("a17aa945be4a0b0c14e0318fb3f45aacceb0c3b5"),
				SourceRev:          utils.PtrFromValue("575b2e9c3c8d3b433260b84328e4f884bcaf7721"),
				RelatedToMergeBase: true,
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{"result": [
    {
      "change": "M",
      "path": ".gitmodules",
      "type": "file"
    },
    {
      "change": "M",
      "path": "renamed_dir",
      "type": "dir"
    },
    {
      "change": "R",
      "path": "renamed_dir/inner",
      "renamedTo": "renamed_dir/inner2",
      "type": "dir"
    },
    {
      "change": "R",
      "path": "renamed_dir/inner/3.txt",
      "renamedTo": "renamed_dir/inner2/3.txt",
      "type": "file"
    },
    {
      "change": "D",
      "path": "renamed_dir/partial_notok",
      "type": "file"
    },
    {
      "change": "A",
      "path": "renamed_dir/partial_notok_2",
      "type": "file"
    },
    {
      "change": "R+M",
      "path": "renamed_dir/partial_ok",
      "renamedTo": "renamed_dir/partial_ok_2",
      "type": "file"
    },
    {
      "change": "D",
      "path": "subdir",
      "type": "submodule"
    }
  ]}`},
		{
			name:     "multiple merge bases",
			repoSlug: "yandex/crisscross",
			body: schemas.TreeDiffRequest{
				TargetRev:          utils.PtrFromValue("F"),
				SourceRev:          utils.PtrFromValue("G"),
				RelatedToMergeBase: true,
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"result": [
					{"change":"M","path":"a1","type":"file"}
				]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.R().
				SetError(&httpErr).
				SetBody(tt.body).
				Post(fmt.Sprintf("/api/v1/repos/%s/treeDiff", tt.repoSlug))

			require.NoError(t, err)
			testutils.PrintResponse(resp)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				assertjson.MatchExact(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})
	}
}

func (suite *LargeRepoApiTestSuite) TestTreeDiff503() {

}
