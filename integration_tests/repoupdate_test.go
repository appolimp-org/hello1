package integrationtests

import (
	"common/testutils/assertjson"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) MakeProject(
	orgSlug, projSlug string, projVisibility entities.Visibility) (uint64, error) {
	ctx := context.Background()

	org, err := suite.OrgRepo.GetOrganization(ctx, orgSlug)
	if err != nil {
		return 0, err
	}

	projID, err := suite.OrgRepo.CreateProject(ctx, &entities.Project{
		Name:       "Project " + projSlug,
		Slug:       projSlug,
		OrgID:      org.ID,
		Visibility: projVisibility,
	})

	if err != nil {
		return 0, err
	}

	return projID, err
}

func (suite *RwApiTestSuite) TestRepoUpdate() {

	t := suite.T()

	testcases := []struct {
		TestName, PostURL, GetURL, ExpectedErrCode string

		Body             *schemas.UpdateRepositoryRequest
		ExpectedStatus   int
		ExpectedResponse string
	}{
		{
			TestName: "BadRequest",
			Body: &schemas.UpdateRepositoryRequest{
				Slug: utils.PtrFromValue("yandex/!!!"),
			},
			PostURL:         "/api/v1/repos/yandex/history",
			ExpectedStatus:  400,
			ExpectedErrCode: httperrors.ErrValidationFailed.ErrorCode,
		},
		{
			TestName: "NotFound",
			Body: &schemas.UpdateRepositoryRequest{
				Slug: utils.PtrFromValue("yandex/newrepo"),
			},
			PostURL:         "/api/v1/repos/yandex/nonexistent",
			ExpectedStatus:  404,
			ExpectedErrCode: httperrors.ErrNotFoundRepository.ErrorCode,
		},
		{
			TestName: "OK",
			Body: &schemas.UpdateRepositoryRequest{
				Slug: utils.PtrFromValue("newrepo"),
				Name: utils.PtrFromValue("YaNdEx/HiHiHi"),
			},
			PostURL:        "/api/v1/repos/yandex/history",
			GetURL:         "/api/v1/repos/yandex/newrepo",
			ExpectedStatus: 200,
			ExpectedResponse: `{
			    "id": "<<PRESENCE>>",
				"name":"YaNdEx/HiHiHi",
				"slug":"newrepo",
				"orgSlug":"yandex",
				"projSlug":null,
				"defaultBranch":"master",
				"visibility": "public",
				"empty": false,
				"cloneURL":{
					"https":"http://git@localhost:8081/yandex/newrepo.git",
					"ssh":"ssh://localhost:2222/yandex/newrepo.git"
				},
				"description": "description is history",
				"logoURL":null
			}`,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.TestName, func(t *testing.T) {
			var details schemas.RepoDetails
			httpErr := &httperrors.APIError{}

			resp, err := suite.client.As(testutils.UserIdentities.Admin).
				SetResult(&details).
				SetBody(tc.Body).SetError(httpErr).Post(tc.PostURL)

			require.NoError(t, err)
			require.Equal(t, tc.ExpectedStatus, resp.StatusCode())
			require.Equal(t, tc.ExpectedErrCode, httpErr.ErrorCode)

			if resp.StatusCode() != 200 {
				return
			}
			assertjson.MatchExact(t, resp.Body(), tc.ExpectedResponse)
		})
	}
}

func (suite *RwApiTestSuite) TestSetDefaultBranch() {
	suite.T().Run("green path", func(t *testing.T) {
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			SetBody(&schemas.UpdateRepositoryRequest{
				DefaultBranch: utils.PtrFromValue("branch"),
			}).Post("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 200, resp.StatusCode())

		var r schemas.RepoDetails
		resp, err = suite.client.R().
			SetResult(&r).Get("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 200, resp.StatusCode())
		require.Equal(suite.T(), "branch", r.DefaultBranch)

	})
	suite.T().Run("non-existent branch", func(t *testing.T) {
		resp, err := suite.client.As(testutils.UserIdentities.Admin).
			SetBody(&schemas.UpdateRepositoryRequest{
				DefaultBranch: utils.PtrFromValue("ahahaahah"),
			}).Post("/api/v1/repos/yandex/alpha")

		require.NoError(suite.T(), err)
		require.Equal(suite.T(), 404, resp.StatusCode())
	})
}

func (suite *RwApiTestSuite) TestRepoIsEmpty() {
	t := suite.T()
	repoSlug := suite.mustGenUniqueSlug()

	var repo schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug: utils.PtrFromValue("yandex"),
			Slug:    repoSlug,
			Name:    repoSlug,
		}).Post("/api/v1/repos")).
		MustBe(t, 201)
	require.True(t, repo.Empty)

	protocol := suite.SSHProtocol()

	tmpDir := testutils.TempDir(t, "", "")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Admin)
	cg.Must(t, "init", ".")
	cg.Must(t, "branch", "-m", "master") // default branch name depends on git version, so specify it explicitly
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL("yandex", repoSlug))
	fname := "test.txt"
	suite.makeNewFile(cg, fname)
	cg.Must(t, "add", "*")
	cg.Must(t, "commit", "-m", "cmt")
	cg.Must(t, "push", "-u", "origin", "master")

	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&repo).
		Get(fmt.Sprintf("/api/v1/repos/yandex/%s", repoSlug))).
		MustBe(t, 200)
	require.False(t, repo.Empty)

	var listRepos schemas.Collection[schemas.RepoDetails]
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetResult(&listRepos).
		Get("/api/v1/orgs/yandex/repos")).
		MustBe(t, 200)

	for _, r := range listRepos.Result {
		if r.OrgSlug == "yandex" && r.Slug == repoSlug {
			require.False(t, r.Empty)
			break
		}
	}
}

func (suite *RwApiTestSuite) TestProjectVisibility() {
	t := suite.T()

	orgSlug := "yandex"
	projSlug := "yandex-private-project"
	repoSlug := "public-repo"

	_, err := suite.MakeProject(
		orgSlug,
		projSlug,
		entities.Visibilities.Internal,
	)

	if err != nil {
		t.Fatal(err)
	}

	var repo schemas.RepoDetails

	testutils.Expect(suite.client.As(suite.users.Admin.Identity).
		SetResult(&repo).
		SetBody(&schemas.CreateRepositoryRequest{
			OrgSlug:    utils.PtrFromValue(orgSlug),
			Slug:       repoSlug,
			Name:       repoSlug,
			ProjSlug:   utils.PtrFromValue(projSlug),
			Visibility: utils.PtrFromValue(entities.Visibilities.Internal),
		}).Post("/api/v1/repos")).
		MustBe(t, 201)
	require.True(t, repo.Empty)

	var r schemas.RepoDetails
	resp, err := suite.client.As(suite.users.Admin.Identity).
		SetResult(&r).Get("/api/v1/repos/yandex/" + repoSlug)

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	require.Equal(t, orgSlug, r.OrgSlug)
	require.Equal(t, repoSlug, r.Slug)
	require.Equal(t, entities.Visibilities.Internal, r.Visibility)
}

func (suite *RwApiTestSuite) TestSetLogo() {
	t := suite.T()

	fileData, err := os.ReadFile(testutils.GetAttachmentPath("img.png"))
	require.NoError(t, err)

	result := &schemas.UploadFileResponse{}
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).
		SetQueryParams(map[string]string{
			"file_name":   "img.png",
			"upload_type": string(schemas.FileUploadTypes.Image),
		}).
		SetHeaders(map[string]string{"Content-Type": "image/png"}).
		SetBody(fileData).SetResult(&result).
		Post("/api/v1/me/uploads")).MustBe(t, http.StatusOK)

	logoKey := result.Key

	var details schemas.RepoDetails
	testutils.Expect(suite.client.As(testutils.UserIdentities.Admin).SetResult(&details).
		SetBody(&schemas.UpdateRepositoryRequest{LogoKey: utils.PtrFromValue(logoKey)}).
		Post("/api/v1/repos/yandex/history")).MustBe(t, http.StatusOK)

	require.NotNil(t, details.LogoURL)
	require.NotEqual(t, "", *details.LogoURL)
}
