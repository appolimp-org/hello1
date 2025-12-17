package integrationtests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
)

func (suite *RwApiTestSuite) TestAuditEventForRepoCreate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	_, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	tt := map[string]struct {
		user         *entities.User
		request      *schemas.CreateRepositoryRequest
		expectedCode int
	}{
		"repo created": {
			user: suite.users.Admin,
			request: &schemas.CreateRepositoryRequest{
				OrgSlug:     utils.PtrFromValue(suite.orgs.Yandex.Slug),
				Slug:        "foo",
				Name:        "foo",
				ProjSlug:    utils.PtrFromValue("goodproj"),
				Description: utils.PtrFromValue("Foo description"),
			},
			expectedCode: 201,
		},
		"repo create error": {
			user: suite.users.Krosh,
			request: &schemas.CreateRepositoryRequest{
				OrgSlug:     utils.PtrFromValue(suite.orgs.Yandex.Slug),
				Slug:        "foo3",
				Name:        "foo3",
				ProjSlug:    utils.PtrFromValue("goodproj"),
				Description: utils.PtrFromValue("Foo description"),
			},
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			var repo schemas.RepoDetails
			testutils.Expect(suite.client.As(tc.user.Identity).
				SetResult(&repo).
				SetBody(tc.request).Post("/api/v1/repos")).
				MustBe(t, tc.expectedCode)

			msgs, err := et.GetCreateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForRepoDelete() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	_, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	tt := map[string]struct {
		user         *entities.User
		path         string
		expectedCode int
	}{
		"repo deleted": {
			user:         suite.users.Admin,
			path:         "yandex/alpha",
			expectedCode: 200,
		},
		"repo delete error": {
			user:         suite.users.Krosh,
			path:         "yandex/alpha-repo-fork",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			resp, err := suite.client.As(tc.user.Identity).Delete("/api/v1/repos/" + tc.path)
			require.NoError(suite.T(), err)
			require.Equal(suite.T(), tc.expectedCode, resp.StatusCode())

			msgs, err := et.GetDeleteRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForRepoUpdate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	_, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	tt := map[string]struct {
		user         *entities.User
		request      *schemas.UpdateRepositoryRequest
		path         string
		expectedCode int
	}{
		"repo update": {
			user: suite.users.Admin,
			request: &schemas.UpdateRepositoryRequest{
				Slug:        utils.PtrFromValue(suite.repos.Alpha.Slug),
				Description: utils.PtrFromValue("bbb"),
				Visibility:  utils.PtrFromValue(entities.Visibilities.Public),
			},
			path:         "yandex/alpha",
			expectedCode: 200,
		},
		"repo update permission error": {
			user: suite.users.Krosh,
			request: &schemas.UpdateRepositoryRequest{
				Description: utils.PtrFromValue("ccc"),
				Visibility:  utils.PtrFromValue(entities.Visibilities.Private),
			},
			path:         "yandex/alpha",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			var details schemas.RepoDetails
			resp, err := suite.client.As(tc.user.Identity).
				SetResult(&details).
				SetBody(tc.request).Post("/api/v1/repos/" + tc.path)

			require.NoError(t, err)
			require.Equal(t, tc.expectedCode, resp.StatusCode())

			msgs, err := et.GetUpdateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}
