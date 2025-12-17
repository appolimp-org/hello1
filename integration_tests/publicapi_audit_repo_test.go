package integrationtests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"common/testutils/yarequire"
	"gitcore/internal/entities"

	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
)

func (suite *RwApiTestSuite) TestPublicApiAuditEventForRepoCreate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	_, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	tt := map[string]struct {
		user         *entities.User
		request      *pbPub.CreateRepositoryBody
		path         string
		expectedCode int
	}{
		"repo created": {
			user: suite.users.Admin,
			request: &pbPub.CreateRepositoryBody{
				Slug:        "foo",
				Name:        "foo",
				Description: "Foo description",
			},
			path:         "/orgs/yandex/repos",
			expectedCode: 201,
		},
		"repo create error": {
			user: suite.users.Krosh,
			request: &pbPub.CreateRepositoryBody{
				Slug:        "foo3",
				Name:        "foo3",
				Description: "Foo description",
			},
			path:         "/orgs/yandex/repos",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			resp, err := suite.gwClient.As(tc.user.Identity).SetBody(tc.request).Post(tc.path)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCode, resp.StatusCode())

			msgs, err := et.GetCreateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestPublicApiAuditEventForRepoDelete() {
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
			path:         "/yandex/alpha",
			expectedCode: 204,
		},
		"repo delete error": {
			user:         suite.users.Krosh,
			path:         "/yandex/alpha-repo-fork",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			resp, err := suite.gwClient.As(tc.user.Identity).Delete(tc.path)
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

func (suite *RwApiTestSuite) TestPublicApiAuditEventForRepoUpdate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	_, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	tt := map[string]struct {
		user         *entities.User
		request      *pbPub.UpdateRepositoryBody
		path         string
		expectedCode int
	}{
		"repo update": {
			user: suite.users.Admin,
			request: &pbPub.UpdateRepositoryBody{
				Description: "bbb",
				Visibility:  pbPub.Repository_internal,
			},
			path:         "yandex/alpha",
			expectedCode: 200,
		},
		"repo update permission error": {
			user: suite.users.Krosh,
			request: &pbPub.UpdateRepositoryBody{
				Description: "bbb",
				Visibility:  pbPub.Repository_private,
			},
			path:         "yandex/alpha",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			resp, err := suite.gwClient.As(tc.user.Identity).
				SetBody(tc.request).Patch(tc.path)

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

func (suite *RwApiTestSuite) TestPublicApiAuditEventForRepoFork() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	tt := map[string]struct {
		user         *entities.User
		request      *pbPub.ForkRepositoryBody
		path         string
		expectedCode int
	}{
		"repo forked": {
			user: suite.users.Admin,
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Yandex.Slug,
				Slug:              "new-fork3",
				DefaultBranchOnly: true,
			},
			path:         "/repos/yandex/alpha/fork",
			expectedCode: 201,
		},
		"repo fork error": {
			user: suite.users.Krosh,
			request: &pbPub.ForkRepositoryBody{
				OrgSlug:           suite.orgs.Yandex.Slug,
				Slug:              "new-fork4",
				DefaultBranchOnly: true,
			},
			path:         "/repos/yandex/alpha/fork",
			expectedCode: 403,
		},
	}

	ctx := context.Background()
	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, et.DeleteAuditEvents(ctx))

			resp, err := suite.gwClient.As(tc.user.Identity).
				SetBody(tc.request).Post(tc.path)

			require.NoError(t, err)
			require.Equal(t, tc.expectedCode, resp.StatusCode())

			msgs, err := et.GetCreateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}
