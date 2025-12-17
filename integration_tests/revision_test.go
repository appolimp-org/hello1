package integrationtests

import (
	"common/testutils/assertjson"
	"gitcore/internal/httpserver/httperrors"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func (suite *RepoApiTestSuite) TestResolveRevision() {
	t := suite.T()

	tests := []struct {
		name             string
		repo             string
		rev              string
		expectedCode     int
		expectedResponse string
		expectedErrCode  httperrors.ErrorCode
	}{
		{
			name:         "main",
			repo:         "yandex/listtags",
			rev:          "main",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"type":"branch",
				"commit":{
					"hash":"94d20dfa7b2ae3c998de92780be3058a68280d76",
					"message":"final\n",
					"author":{
						"name":"Author",
						"email":"author@example.com",
						"date":"2024-02-19T12:05:00Z"
					},
					"committer":{
						"name":"Commiter",
						"email":"committer@example.com",
						"date":"2024-02-19T12:05:00Z"
					},
					"parentCommits": [
      					"78350efe20fc33f940c2894e7c74e15963d7356a"
					]
				},
				"branches":["main"],
				"tags":["v3-an"]
			}`,
		},
		{
			name:         "tag",
			repo:         "yandex/listtags",
			rev:          "tag:v2.main",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"type": "tag",
				"commit": {
					"hash":"78350efe20fc33f940c2894e7c74e15963d7356a",
					"message":"main 2\n",
					"author":{
						"name":"Author",
						"email":"author@example.com",
						"date":"2024-02-19T12:03:20Z"
					},
					"committer":{
						"name":"Commiter",
						"email":"committer@example.com",
						"date":"2024-02-19T12:03:20Z"
					},
					"parentCommits": [
						  "39427c55a3c59b91b11b5c1413033ebb85f84364"
					]
				},
				"tags":[
					"v2.main",
					"v2.main-an"
				]
			}`,
		},
		{
			name:         "tag_annotated",
			repo:         "yandex/listtags",
			rev:          "tag:v2.main-an",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"type": "tag",
				"commit": {
					"hash":"78350efe20fc33f940c2894e7c74e15963d7356a",
					"message":"main 2\n",
					"author":{
						"name":"Author",
						"email":"author@example.com",
						"date":"2024-02-19T12:03:20Z"
					},
					"committer":{
						"name":"Commiter",
						"email":"committer@example.com",
						"date":"2024-02-19T12:03:20Z"
					},
  					"parentCommits": [
      					"39427c55a3c59b91b11b5c1413033ebb85f84364"
    				]
				},
				"tags":[
					"v2.main",
					"v2.main-an"
				]
			}`,
		},
		{
			name:         "commit",
			repo:         "yandex/listtags",
			rev:          "39427c55a3c59b91b11b5c1413033ebb85f84364",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"type": "commit",
				"commit": {
					"hash":"39427c55a3c59b91b11b5c1413033ebb85f84364",
					"message":"v1\n",
					"author":{
						"name":"Author",
						"email":"author@example.com",
						"date":"2024-02-19T12:00:00Z"
					},
					"committer":{
						"name":"Commiter",
						"email":"committer@example.com",
						"date":"2024-02-19T12:00:00Z"
					},
 					"parentCommits": []

				},
				"tags":["v1.0"]
			}`,
		},
		{
			name:         "without_refs",
			repo:         "yandex/blame",
			rev:          "43683522b7adcfb445b2c9323083c8a2f9cde4b8",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"type": "commit",
				"commit": {
					"hash":"43683522b7adcfb445b2c9323083c8a2f9cde4b8",
					"message":"first commit\n",
					"author":{
						"name":"Author",
						"email":"author@example.com",
						"date":"2024-02-19T12:00:00Z"
					},
					"committer":{
						"name":"Commiter",
						"email":"committer@example.com",
						"date":"2024-02-19T12:00:00Z"
					},
					"parentCommits": []
				}
			}`,
		},
		{
			name:         "multiple parents (merge)",
			repo:         "yandex/alpha",
			rev:          "1669dce138d9b841a518c64b10914d88f5e488ea",
			expectedCode: http.StatusOK,
			expectedResponse: `{
				"commit": {
					"author": {
					  "date": "2015-03-31T11:48:14Z",
					  "email": "mcuadros@gmail.com",
					  "name": "Máximo Cuadros Ortiz"
					},
				"committer": {
					  "date": "2015-03-31T11:48:14Z",
					  "email": "mcuadros@gmail.com",
					  "name": "Máximo Cuadros Ortiz"
					},
				"hash": "1669dce138d9b841a518c64b10914d88f5e488ea",
				"message": "Merge branch 'master' of github.com:tyba/git-fixture\n",
				"parentCommits": [
					  "35e85108805c84807bc66a02d91535e1e24b38b9",
					  "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69"
					]
				},
				"type": "commit"
			}`,
		},
		{
			name:            "empty",
			repo:            "yandex/listtags",
			rev:             "",
			expectedCode:    http.StatusBadRequest,
			expectedErrCode: httperrors.ErrorCodeValidationFailed,
		},
		{
			name:            "not_found_org",
			repo:            "unknown-org/listtags",
			rev:             "39427c55a3c59b91b11b5c1413033ebb85f84364",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundOrganization,
		},
		{
			name:            "not_found_repo",
			repo:            "yandex/unknown-repo",
			rev:             "39427c55a3c59b91b11b5c1413033ebb85f84364",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundRepository,
		},
		{
			name:            "not_found_commit",
			repo:            "yandex/listtags",
			rev:             "111112222200000000000000000000000000000",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundRevision,
		},
		{
			name:            "not_found_branch",
			repo:            "yandex/listtags",
			rev:             "unknown_branch",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundRevision,
		},
		{
			name:            "not_found_tag",
			repo:            "yandex/listtags",
			rev:             "v9.9",
			expectedCode:    http.StatusNotFound,
			expectedErrCode: httperrors.ErrorCodeNotFoundRevision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.R().
				SetError(&httpErr).
				SetQueryParam("rev", tt.rev).
				Get("/api/v1/repos/" + tt.repo + "/resolveRevision")

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				assertjson.MatchExact(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, string(tt.expectedErrCode), httpErr.ErrorCode)
			}
		})
	}
}
