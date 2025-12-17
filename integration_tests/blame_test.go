package integrationtests

import (
	"common/logging"
	"common/testutils/assertjson"
	commonutils "common/utils"
	"context"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/utils"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"testing"
)

func (suite *RepoApiTestSuite) TestSmall() {
	t := suite.T()

	tests := []struct {
		name             string
		body             schemas.BlameRequest
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
	}{
		{
			name: "rename",
			body: schemas.BlameRequest{
				Rev:  commonutils.PtrFromValue("master"),
				Path: "file-rename.txt",
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
					"ranges": [
						{ "loc": [1, 1, 1, 1], "commitOid": "43683522b7adcfb445b2c9323083c8a2f9cde4b8", "reblamePath": "file.txt" },
						{ "loc": [2, 1, 2, 1], "commitOid": "efc7d51346c9dc1dfcc2c8881b2e4ed8fea53d9e", "reblamePath": "file.txt" },
						{ "loc": [3, 1, 3, 1], "commitOid": "256b095a4cee4a9e19da2e736586d92f6c6224b1", "reblamePath": "file-rename.txt" }
					]
			}`,
		},
		{
			name: "insert and delete",
			body: schemas.BlameRequest{
				Rev:  commonutils.PtrFromValue("master"),
				Path: "file3.txt",
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
					"ranges": [
						{ "loc": [1, 6, 1, 4], "commitOid": "43683522b7adcfb445b2c9323083c8a2f9cde4b8", "reblamePath": "file3.txt" },
						{ "loc": [5, 2, 5, 2], "commitOid": "256b095a4cee4a9e19da2e736586d92f6c6224b1", "reblamePath": "file3.txt" },
						{ "loc": [7, 2, 7, 2], "commitOid": "43683522b7adcfb445b2c9323083c8a2f9cde4b8", "reblamePath": "file3.txt" }
					]
			}`,
		},
		{
			name: "line filter",
			body: schemas.BlameRequest{
				Rev:  commonutils.PtrFromValue("master"),
				Path: "file3.txt",
				From: commonutils.PtrFromValue(int32(5)),
				To:   commonutils.PtrFromValue(int32(7)),
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
					"ranges": [
						{ "loc": [5, 2, 5, 2], "commitOid": "256b095a4cee4a9e19da2e736586d92f6c6224b1", "reblamePath": "file3.txt" }
					]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}
			response := &schemas.Collection[schemas.BlameResponse]{}
			request := suite.client.R().
				SetError(&httpErr).
				SetResult(response).
				SetQueryParam("path", tt.body.Path).
				SetQueryParam("rev", *tt.body.Rev)

			if tt.body.From != nil {
				request = request.SetQueryParam("from", strconv.Itoa(int(*tt.body.From)))
			}
			if tt.body.To != nil {
				request = request.SetQueryParam("to", strconv.Itoa(int(*tt.body.To)))
			}

			resp, err := request.Get("/api/v1/repos/yandex/blame/blame")

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				assertjson.Match(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})
	}
}

func (suite *LargeRepoApiTestSuite) TestLargeWithOdyssey() {
	t := suite.T()

	tests := []struct {
		name             string
		body             schemas.BlameRequest
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
	}{
		{
			name: ".gitignore",
			body: schemas.BlameRequest{
				Rev:  commonutils.PtrFromValue("9d0de5d4e5ef1c3489570497c847845a95be7dc9"),
				Path: ".gitignore",
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
						"ranges": [
								{ "loc": [1, 1, 1, 1], "commitOid": "331f3693aadf5131f202e8beaada842f0bf759ba", "reblamePath": ".gitignore"},
								{ "loc": [2, 2, 2, 2], "commitOid": "6a434638823c4639b79d0fc7deca5dc79d005216", "reblamePath": ".gitignore"},
								{ "loc": [4, 1, 4, 1], "commitOid": "debfd09d8ebbb431b48ca4dba7fdd732482c59ab", "reblamePath": ".gitignore"},
								{ "loc": [5, 1, 5, 1], "commitOid": "0933d1ebb3ea0b7f3107e5841eb7b67e6e2cd92c", "reblamePath": ".gitignore"},
								{ "loc": [6, 1, 6, 1], "commitOid": "71c98b23a8872aed85d6bc76a198e00ebe0f1fdd", "reblamePath": ".gitignore"},
								{ "loc": [6, 2, 7, 2], "commitOid": "0933d1ebb3ea0b7f3107e5841eb7b67e6e2cd92c", "reblamePath": ".gitignore"},
								{ "loc": [9, 1, 9, 1], "commitOid": "f0ee140956ff703c4fdde7e9055bfde5ec093203", "reblamePath": ".gitignore"},
								{ "loc": [2, 1, 10, 1], "commitOid": "f108dcba09e37f618c218d3faa4ba7a592a335bf", "reblamePath": ".gitignore"},
								{ "loc": [6, 1, 11, 1], "commitOid": "c6542d7003e98780297750a952ff97ab3a08d67f", "reblamePath": ".gitignore"},
								{ "loc": [6, 1, 12, 1], "commitOid": "7e0cf28b7927e4884d6764e5d5c5e1106ee557d1", "reblamePath": ".gitignore"},
								{ "loc": [7, 1, 13, 1], "commitOid": "8518e9a959a0c7038d4a8ed27ce50bfda67f6698", "reblamePath": ".gitignore"},
								{ "loc": [9, 1, 14, 1], "commitOid": "95f174003117018073ab16d4c57478d19d6cab5b", "reblamePath": ".gitignore"},
								{ "loc": [7, 2, 15, 2], "commitOid": "64fb6c1f47c587f542ce70180d6a96bc919f0c52", "reblamePath": ".gitignore"},
								{ "loc": [15, 1, 17, 1], "commitOid": "c95f0bce191f09cb55a79016d2f7fc160b73fb8f", "reblamePath": ".gitignore"},
								{ "loc": [9, 2, 18, 2], "commitOid": "64fb6c1f47c587f542ce70180d6a96bc919f0c52", "reblamePath": ".gitignore"},
								{ "loc": [13, 1, 20, 1], "commitOid": "debfd09d8ebbb431b48ca4dba7fdd732482c59ab", "reblamePath": ".gitignore"},
								{ "loc": [15, 1, 21, 1], "commitOid": "bc568250bf66101ce6d8b844f30fb5d12f12424c", "reblamePath": ".gitignore"},
								{ "loc": [16, 1, 22, 1], "commitOid": "0709d7eb26501ac655853e8d60f0237fb055cf39", "reblamePath": ".gitignore"},
								{ "loc": [17, 1, 23, 1], "commitOid": "785e85ab6f9d0e1c80208118b4f9bfe46c63a399", "reblamePath": ".gitignore"},
								{ "loc": [24, 1, 24, 1], "commitOid": "8a3831b082a8aca8e7ca90723e43e0022e237fdd", "reblamePath": ".gitignore"},
								{ "loc": [25, 1, 25, 1], "commitOid": "d8b0cee67cdf69b5e8936047965f7cf878e64285", "reblamePath": ".gitignore"},
								{ "loc": [26, 2, 26, 2], "commitOid": "74623edfb5b29bb8ec5b073b1b2e85ad4a669730", "reblamePath": ".gitignore"},
								{ "loc": [27, 3, 28, 3], "commitOid": "ac6fe08e02cebcea259b2630a5a5217453860767", "reblamePath": ".gitignore"},
								{ "loc": [31, 1, 31, 1], "commitOid": "9a423940b9dc04d17e3a4fbd975f3e5d6acc4ae0", "reblamePath": ".gitignore"}
						]
			}`,
		},
		{
			name: "sources/CMakeLists.txt",
			body: schemas.BlameRequest{
				Rev:  commonutils.PtrFromValue("9d0de5d4e5ef1c3489570497c847845a95be7dc9"),
				Path: "sources/CMakeLists.txt",
			},
			expectedCode: http.StatusOK,
			expectedResponse: `{
						"ranges": [
									{"loc": [1, 3, 1, 3], "commitOid":  "f0300934847de1726cab36617d0fc481bc31fec6", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [2, 1, 4, 1], "commitOid":  "ddc1d0a06b8911b177ef55482871c6cc68156222", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [3, 1, 5, 1], "commitOid":  "b57bc4ddd7bd65cea1961cc4ae128c7aa1a145d4", "reblamePath":  "src/CMakeLists.txt"},
									{"loc": [7, 1, 6, 1], "commitOid":  "9e096b96b2bfca4b303e193459c0ed28bd117638", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [5, 1, 7, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [6, 1, 8, 1], "commitOid":  "7e217b3b5aefd1d88bf9135e25d44b5191bf614f", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [9, 1, 9, 1], "commitOid":  "a9a6c0704433c6dc47b23b1801eca5433d828e6d", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [8, 1, 10, 1], "commitOid":  "2e03af9c88d45c8849ddb0929bbd48b8051417ad", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [7, 1, 11, 1], "commitOid":  "bb826db7e895f06c54251ccad28394e5ca07291a", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [8, 1, 12, 1], "commitOid":  "4df9f8e760ef6435ba495c79016a4af0e37ed764", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [11, 1, 13, 1], "commitOid":  "6c0a1f09f82e38e10830d0728d76761290c69d42", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [18, 1, 14, 1], "commitOid":  "9e096b96b2bfca4b303e193459c0ed28bd117638", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [16, 1, 15, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [14, 1, 16, 1], "commitOid":  "2e03af9c88d45c8849ddb0929bbd48b8051417ad", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [18, 1, 17, 1], "commitOid":  "d48cd092a03b360d85b79b273d94cbb924326b82", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [18, 1, 18, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [16, 1, 19, 1], "commitOid":  "1d5d1b5c21683f15e2c4cd2f606e219c3c221c2c", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [24, 1, 20, 1], "commitOid":  "fdd39f76e66e9503082313acf124d21b658b6fd2", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [20, 1, 21, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [25, 1, 22, 1], "commitOid":  "9e096b96b2bfca4b303e193459c0ed28bd117638", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [20, 1, 23, 1], "commitOid":  "2e03af9c88d45c8849ddb0929bbd48b8051417ad", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [23, 1, 24, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [22, 1, 25, 1], "commitOid":  "2e03af9c88d45c8849ddb0929bbd48b8051417ad", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [25, 1, 26, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [27, 1, 27, 1], "commitOid":  "9d0de5d4e5ef1c3489570497c847845a95be7dc9", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [24, 1, 28, 1], "commitOid":  "2e03af9c88d45c8849ddb0929bbd48b8051417ad", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [27, 1, 29, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [26, 1, 30, 1], "commitOid":  "41a5449969423437da3dd9daaada33bea81e9d89", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [28, 1, 31, 1], "commitOid":  "60d7229cdcbdf50968d1c3e5e83d0c8c047add5e", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [28, 1, 32, 1], "commitOid":  "b51317b2547f61b4c5b4f6a6bf0502beb6807ed4", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [29, 2, 33, 2], "commitOid":  "66c1c63751a9c73c368832efc0cbda50bbfea25a", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [34, 4, 35, 4], "commitOid":  "f0300934847de1726cab36617d0fc481bc31fec6", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [38, 1, 39, 1], "commitOid":  "52b0abb647b4c11a240f3484c749dbbfaf81e77d", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [39, 1, 40, 1], "commitOid":  "f0300934847de1726cab36617d0fc481bc31fec6", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [39, 1, 41, 1], "commitOid":  "222da0c70ac71798346919997da32bcb77aa15cb", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [40, 2, 42, 2], "commitOid":  "b546551bb863c42c4d873b0b9841796ee8299ff8", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [42, 1, 44, 1], "commitOid":  "2019c6419e247877615cae680df59bb519cc0a8e", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [43, 1, 45, 1], "commitOid":  "4630c5d72fc086c68dba6d2ff71480259e00ee97", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [45, 2, 46, 2], "commitOid":  "ba6513323fcea9af44955a7d2635dd8821150ec6", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [47, 2, 48, 2], "commitOid":  "fade74703a16758e8330ccc26f6c90787f1f3e9e", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [49, 1, 50, 1], "commitOid":  "f9a1ec21bc908ce4814b3ca2cfc7f1028204e466", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [50, 1, 51, 1], "commitOid":  "f60df91a5405cb25e77f794175e1a6f3c939ddc9", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [50, 2, 52, 2], "commitOid":  "f9a1ec21bc908ce4814b3ca2cfc7f1028204e466", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [52, 2, 54, 2], "commitOid":  "0e44d132901716db19a47e88903de243df098424", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [28, 1, 56, 1], "commitOid":  "b57bc4ddd7bd65cea1961cc4ae128c7aa1a145d4", "reblamePath":  "src/CMakeLists.txt"},
									{"loc": [28, 1, 57, 1], "commitOid":  "c6542d7003e98780297750a952ff97ab3a08d67f", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [45, 1, 58, 1], "commitOid":  "74623edfb5b29bb8ec5b073b1b2e85ad4a669730", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [30, 2, 59, 2], "commitOid":  "c6542d7003e98780297750a952ff97ab3a08d67f", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [46, 1, 61, 1], "commitOid":  "b071fd1633cad9df06605aad63505b9aff9ca0f8", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [49, 5, 62, 5], "commitOid":  "74623edfb5b29bb8ec5b073b1b2e85ad4a669730", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [47, 2, 67, 2], "commitOid":  "7135f2f0c822f8bdfb70b8b875a02a3ac2b0e421", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [57, 4, 69, 4], "commitOid":  "02f31442656413c98389cff3cf1b186624e5ba0d", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [33, 1, 73, 1], "commitOid":  "9e096b96b2bfca4b303e193459c0ed28bd117638", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [34, 1, 74, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [34, 1, 75, 1], "commitOid":  "9e096b96b2bfca4b303e193459c0ed28bd117638", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [36, 1, 76, 1], "commitOid":  "da7e6ea7a5522a24f781cbbec7de1b65667ce8fc", "reblamePath":  "sources/CMakeLists.txt"},
									{"loc": [33, 11, 77, 10], "commitOid":  "b57bc4ddd7bd65cea1961cc4ae128c7aa1a145d4", "reblamePath":  "src/CMakeLists.txt"},
									{"loc": [64, 4, 87, 4], "commitOid":  "e97f7e1f1c19ff1f06a807da5b64d247abc20359", "reblamePath":  "sources/CMakeLists.txt"}
						]
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := utils.Timeit(context.Background(), "request timing")
			httpErr := httperrors.APIError{}
			response := &schemas.BlameResponse{}
			resp, err := suite.client.R().
				SetError(&httpErr).
				SetResult(response).
				SetQueryParam("path", tt.body.Path).
				SetQueryParam("rev", *tt.body.Rev).
				Get("/api/v1/repos/yandex/odyssey/blame")
			st()

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			logging.Info(nil, string(resp.Body()))
			if tt.expectedResponse != "" {
				assertjson.Match(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})
	}
}
