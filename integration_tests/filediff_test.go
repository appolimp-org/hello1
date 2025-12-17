package integrationtests

import (
	"common/testutils/assertjson"
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestFileDiff() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: utils.PtrFromValue("branch"),
		SourceRev: utils.PtrFromValue("master"),
		Paths:     []string{"README", "vendor/foo.go", "no/such/file.go", "vendor", "binary.jpg"},
		Context:   utils.PtrFromValue(5),
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/alpha/fileDiff")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())

	expected := `{
		"result": [
			{
				"hunks":[
					{"loc":[1,1,0,0],"patch":"-# README\n"}
				],
				"lines_count":{"target":1,"source":0},
				"path":"README",
				"diffPath":{"source":"README","target":"README"},
				"stats":{"added":0,"removed":1}
			},
			{
				"hunks":[
					{"loc":[0,0,1,7],"patch":"+package main\n+\n+import \"fmt\"\n+\n+func main() {\n+\tfmt.Println(\"Hello, playground\")\n+}\n"}
				],
				"lines_count":{"target":0,"source":7},
				"path":"vendor/foo.go",
				"diffPath":{"source":"vendor/foo.go","target":"vendor/foo.go"},
				"stats":{"added":7,"removed":0}
			},
			{
				"failureReason":"fileNotFound",
				"path":"no/such/file.go",
				"diffPath":{"source":"no/such/file.go","target":"no/such/file.go"}
			},
			{
				"failureReason":"badMode",
				"path":"vendor",
				"diffPath":{"source":"vendor","target":"vendor"}
			},
			{
				"failureReason":"binary",
				"path":"binary.jpg",
				"diffPath":{"source":"binary.jpg","target":"binary.jpg"}
			}
		]
	}`
	assertjson.MatchExact(t, resp.Body(), expected)
}

func (suite *RepoApiTestSuite) TestFileDiffFromBase() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev:          utils.PtrFromValue("master"),
		SourceRev:          utils.PtrFromValue("branch"),
		Paths:              []string{"README", "vendor/foo.go", "no/such/file.go", "vendor", "binary.jpg"},
		Context:            utils.PtrFromValue(5),
		RelatedToMergeBase: true,
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/alpha/fileDiff")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())

	expected := `{
		"result": [
			{
				"hunks":[
					{"loc":[0,0,1,1],"patch":"+# README\n"}
				],
				"lines_count":{"target":0,"source":1},
				"path":"README",
				"diffPath":{"source":"README","target":"README"},
				"stats":{"added":1,"removed":0}
			},
			{
				"failureReason":"fileNotFound",
				"path":"vendor/foo.go",
				"diffPath":{"source":"vendor/foo.go","target":"vendor/foo.go"}
			},
			{
				"failureReason":"fileNotFound",
				"path":"no/such/file.go",
				"diffPath":{"source":"no/such/file.go","target":"no/such/file.go"}
			},
			{
				"failureReason":"fileNotFound",
				"path":"vendor",
				"diffPath":{"source":"vendor","target":"vendor"}
			},
			{
				"failureReason":"binary",
				"path":"binary.jpg",
				"diffPath":{"source":"binary.jpg","target":"binary.jpg"}
			}
		]
	}`
	assertjson.MatchExact(t, resp.Body(), expected)
}

func (suite *RepoApiTestSuite) TestFileDiffBadRev() {
	t := suite.T()

	httpErr := &httperrors.APIError{}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(schemas.FileDiffRequest{SourceRev: utils.PtrFromValue("master"), TargetRev: utils.PtrFromValue("nosuchrev"), Paths: []string{"README"}}).
		Post("/api/v1/repos/yandex/alpha/fileDiff")
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode())
	require.Equal(t, httperrors.ErrNotFoundRevision.ErrorCode, httpErr.ErrorCode)

	resp, err = suite.client.R().
		SetError(httpErr).
		SetBody(schemas.FileDiffRequest{SourceRev: utils.PtrFromValue("nosuchrev"), TargetRev: utils.PtrFromValue("master"), Paths: []string{"README"}}).
		Post("/api/v1/repos/yandex/alpha/fileDiff")
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode())
	require.Equal(t, httperrors.ErrNotFoundRevision.ErrorCode, httpErr.ErrorCode)
}

func (suite *RepoApiTestSuite) TestFileDiffNilTarget() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: nil,
		SourceRev: utils.PtrFromValue("branch"),
		Paths:     []string{"README"},
		Context:   utils.PtrFromValue(5),
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/alpha/fileDiff")

	yarequire.StatusCode(t, resp, err, 200)

	expected := `{
		"result": [
			{
				"hunks":[
					{"loc":[0,0,1,1],"patch":"+# README\n"}
				],
				"lines_count":{"target":0,"source":1},
				"path":"README",
				"diffPath":{"source":"README","target":"README"},
				"stats":{"added":1,"removed":0}
			}
		]
	}`
	assertjson.MatchExact(t, resp.Body(), expected)
}

func (suite *RepoApiTestSuite) TestFileDiffNilSource() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: utils.PtrFromValue("branch"),
		SourceRev: nil,
		Paths:     []string{"README"},
		Context:   utils.PtrFromValue(5),
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/alpha/fileDiff")

	yarequire.StatusCode(t, resp, err, 400)
}

func (suite *RepoApiTestSuite) TestFileDiffTooBig() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: utils.PtrFromValue("main"),
		SourceRev: utils.PtrFromValue("feature"),
		Paths:     []string{"toobig", "okay"},
		Context:   utils.PtrFromValue(0),
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/bigdiff/fileDiff")

	yarequire.StatusCode(t, resp, err, 200)

	expected := `{
		"result": [
			{
					"failureReason":"diffSizeLimitReached",
					"path":"toobig"
			},
			{
				"lines_count":{"source":200,"target":200},"path":"okay","stats":{"added":100,"removed":100}
			}
		]
	}`
	assertjson.Match(t, resp.Body(), expected)
}

func (suite *RepoApiTestSuite) TestFileDiffWoLimit() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: utils.PtrFromValue("feature"),
		SourceRev: utils.PtrFromValue("main"),
		PathsEx: []schemas.FileDiffPath{
			{Source: "toobig", Target: "toobig"},
		},
		Context: utils.PtrFromValue(0),
		NoLimit: true,
	}

	var resp schemas.Collection[schemas.FileDiffResponse]
	testutils.Expect(suite.client.R().
		SetError(httpErr).
		SetResult(&resp).
		SetBody(body).
		Post("/api/v1/repos/yandex/bigdiff/fileDiff")).
		MustBe(t, 200)

	require.Len(t, resp.Result, 1)
	require.Len(t, resp.Result[0].Hunks, 500) // That's what git returns

}

func (suite *RepoApiTestSuite) TestFileDiffRenamed() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev: utils.PtrFromValue("branch"),
		SourceRev: utils.PtrFromValue("main"),
		PathsEx: []schemas.FileDiffPath{
			{Source: "file1.txt", Target: "file1_moved.txt"},
			{Source: "file1.txt", Target: "file3.txt"},
		},
		Context: utils.PtrFromValue(0),
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/differentdiffs/fileDiff")

	yarequire.StatusCode(t, resp, err, 200)

	expected := `{
		"result": [
			{
				"path":"file1.txt",
				"diffPath":{"source":"file1.txt","target":"file1_moved.txt"},
				"lines_count":{"source":10,"target":10},
				"stats":{"added":0,"removed":0}
			},{
				"path":"file1.txt",
				"diffPath":{"source":"file1.txt","target":"file3.txt"},
				"lines_count":{"source":10,"target":9},
				"stats":{"added":7,"removed":6},
				"hunks":[
					{"loc":[1,2,1,1], "patch":"-Line 6.\n-Line 7.\n+Line 1.\n"},
					{"loc":[3,0,3,1], "patch":"+Line 3.\n"},
					{"loc":[4,0,5,5], "patch":"+Line 5.\n+Line 6.\n+Line 7.\n+Line 8.\n+Line 9.\n"},
					{"loc":[6,4,10,0],"patch":"-Line 11.\n-Line 12.\n-Line 14.\n-Line 15.\n"}
				]
			}
		]
	}`
	assertjson.Match(t, resp.Body(), expected)
}

func (suite *RepoApiTestSuite) TestFileDiffMultipleMergeBases() {
	t := suite.T()
	httpErr := &httperrors.APIError{}

	body := schemas.FileDiffRequest{
		TargetRev:          utils.PtrFromValue("F"),
		SourceRev:          utils.PtrFromValue("G"),
		Paths:              []string{"a1"},
		Context:            utils.PtrFromValue(0),
		RelatedToMergeBase: true,
	}

	resp, err := suite.client.R().
		SetError(httpErr).
		SetBody(body).
		Post("/api/v1/repos/yandex/crisscross/fileDiff")

	yarequire.StatusCode(t, resp, err, 200)

	expected := `{
		"result": [
			{
				"path":"a1",
				"diffPath":{"source":"a1","target":"a1"},
				"lines_count":{"source":1,"target":1},
				"stats":{"added":1,"removed":1},
				"hunks":[{"loc":[1,1,1,1], "patch":"-C\n+G\n"}]
			}
		]
	}`
	assertjson.Match(t, resp.Body(), expected)
}
