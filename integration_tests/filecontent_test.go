package integrationtests

import (
	"gitcore/internal/httpserver/httperrors"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"testing"
)

func (suite *RepoApiTestSuite) TestFileContext() {
	t := suite.T()

	type params struct {
		Path     string
		Revision string
		From     int
		To       int
	}

	tests := []struct {
		name             string
		params           params
		expectedCode     int
		expectedResponse string
		expectedErrCode  string

		expectedContentDisposition string
	}{
		{
			name: "full",
			params: params{
				Path: "vendor/foo.go",
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, playground\")\n}\n",

			expectedContentDisposition: "attachment; filename=\"vendor/foo.go\"",
		},
		{
			name: "interval",
			params: params{
				Path:     "LICENSE",
				Revision: "branch",
				From:     14,
				To:       16,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "THE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR\nIMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,",

			expectedContentDisposition: "attachment; filename=LICENSE",
		},
		{
			name: "from_start",
			params: params{
				Path:     "LICENSE",
				Revision: "branch",
				To:       2,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "The MIT License (MIT)\n",

			expectedContentDisposition: "attachment; filename=LICENSE",
		},
		{
			name: "to_end",
			params: params{
				Path:     "LICENSE",
				Revision: "branch",
				From:     19,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE\nSOFTWARE.\n\n",

			expectedContentDisposition: "attachment; filename=LICENSE",
		},
		{
			name: "interval_empty",
			params: params{
				Path:     "LICENSE",
				Revision: "branch",
				From:     100,
				To:       1,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "",

			expectedContentDisposition: "attachment; filename=LICENSE",
		},
		{
			name: "interval_from_out", // empty response
			params: params{
				Path: "vendor/foo.go",
				From: 100,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "",

			expectedContentDisposition: "attachment; filename=\"vendor/foo.go\"",
		},
		{
			name: "interval_to_out", // fallback to max_len
			params: params{
				Path: "vendor/foo.go",
				To:   100,
			},
			expectedCode:     http.StatusOK,
			expectedResponse: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, playground\")\n}\n",

			expectedContentDisposition: "attachment; filename=\"vendor/foo.go\"",
		},
		{
			name: "invalid_from",
			params: params{
				Path: "vendor/foo.go",
				From: -10,
			},
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: httperrors.ErrValidationFailed.ErrorCode,
		},
		{
			name: "invalid_to",
			params: params{
				Path: "vendor/foo.go",
				To:   -10,
			},
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: httperrors.ErrValidationFailed.ErrorCode,
		},
		{
			name: "invalid_path",
			params: params{
				Path: "",
			},
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: httperrors.ErrValidationFailed.ErrorCode,
		},
		{
			name: "not_found_path",
			params: params{
				Path: "vendor/not_found.txt",
			},
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrNotFoundPath.ErrorCode,
		},
		{
			name: "not_found_revision",
			params: params{
				Path:     "vendor/foo.go",
				Revision: "not_found_branch",
			},
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrNotFoundRevision.ErrorCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			request := suite.client.R().
				SetError(&httpErr).
				SetQueryParam("path", tt.params.Path)
			if tt.params.Revision != "" {
				request = request.SetQueryParam("rev", tt.params.Revision)
			}
			if tt.params.From != 0 {
				request = request.SetQueryParam("from", strconv.Itoa(tt.params.From))
			}
			if tt.params.To != 0 {
				request = request.SetQueryParam("to", strconv.Itoa(tt.params.To))
			}

			resp, err := request.Get("/api/v1/repos/yandex/alpha/fileContent")

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				require.Equal(t, tt.expectedResponse, string(resp.Body()))
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
			if tt.expectedContentDisposition != "" {
				require.Equal(t, tt.expectedContentDisposition, resp.Header().Get(echo.HeaderContentDisposition))
			}
		})
	}
}
