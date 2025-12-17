package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RepoApiTestSuite) TestImportOrg() {
	t := suite.T()

	t.Run("double create", func(t *testing.T) {
		res := schemas.Organization{}
		resp, err := suite.client.R().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "yc.organization-manager.yandex",
					Src: entities.IdentityProviders.IAM,
				},
				Slug: utils.PtrFromValue("yandex42"),
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.Equal(t, http.StatusConflict, resp.StatusCode())
	})

	t.Run("no auth", func(t *testing.T) {
		res := schemas.Organization{}
		resp, err := suite.client.RWoAuth().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "yc.organization-manager.yandex",
					Src: entities.IdentityProviders.IAM,
				},
				Slug: utils.PtrFromValue("yandex42"),
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.True(t, resp.IsError())
		require.Equal(t, http.StatusConflict, resp.StatusCode())
	})

	t.Run("autogenerate slug and resolve collision", func(t *testing.T) {
		res := schemas.Organization{}
		resp, err := suite.client.R().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "deadbeef",
					Src: entities.IdentityProviders.IAM,
				},
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode())
		require.Equal(t, "azazaza", res.Slug)
		require.NotNil(t, res.Identity)
		require.Equal(t, entities.IdentityProviders.IAM, res.Identity.Src)

		resp, err = suite.client.R().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "deadbabe",
					Src: entities.IdentityProviders.IAM,
				},
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode())
		require.True(t, strings.HasPrefix(res.Slug, "azazaza-"))
		require.NotNil(t, res.Identity)
		require.Equal(t, entities.IdentityProviders.IAM, res.Identity.Src)

	})

	t.Run("slug explicitly specified and clashing", func(t *testing.T) {
		res := schemas.Organization{}

		resp, err := suite.client.R().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "slugclash",
					Src: entities.IdentityProviders.IAM,
				},
				Slug: utils.PtrFromValue("slugclash"),
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode())

		resp, err = suite.client.R().
			SetResult(&res).
			SetBody(&schemas.ImportExternalOrganizationRequest{
				Identity: entities.OrganizationIdentity{
					ID:  "slugclash2",
					Src: entities.IdentityProviders.IAM,
				},
				Slug: utils.PtrFromValue("slugclash"),
			}).
			Post("/api/v1/orgs/import")

		require.NoError(t, err)
		require.Equal(t, http.StatusConflict, resp.StatusCode())
	})
	t.Run("no import during repo creation", func(t *testing.T) {
		res := schemas.Organization{}

		OrgIdentity := entities.OrganizationIdentity{
			ID:  "implicit_import",
			Src: entities.IdentityProviders.IAM,
		}
		resp, err := suite.client.R().
			SetResult(&res).
			SetBody(&schemas.CreateRepositoryRequest{
				Name:        "MegaRepo",
				Slug:        "xxxyyyzzz",
				OrgIdentity: &OrgIdentity,
				Description: utils.PtrFromValue(""),
			}).
			Post("/api/v1/repos/")

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})
}
