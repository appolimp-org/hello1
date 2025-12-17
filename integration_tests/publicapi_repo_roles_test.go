package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListRepoRoles() {
	t := suite.T()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Barash, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	t.Run("list roles", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/roles", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")
	})

	t.Run("list roles by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")
	})

	t.Run("access denied", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Pikachu.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/roles", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	var token string

	t.Run("with pagination", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParams(map[string]string{
				"page_size": "2",
			}).
			Get(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		require.NoError(t, err)
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/next_page_token", "**/id")

		token, err = yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
		require.NoError(t, err)
	})

	t.Run("pagination token", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParams(map[string]string{
				"page_size":  "2",
				"page_token": token,
			}).
			Get(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/next_page_token", "**/id")
	})
}

func (suite *RwApiTestSuite) TestPublicAPIAddRepoRoles() {
	t := suite.T()

	users := []*entities.User{suite.users.Admin, suite.users.Kopatych}

	t.Run("add a role", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_admin,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Kopatych.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/%s/%s/roles", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})

	users = append(users, suite.users.Barash)

	t.Run("add a role by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_developer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Barash.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})

	t.Run("invalid permissions", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Slowpoke.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_developer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	users = append(users, suite.users.Krosh, suite.users.Slowpoke, suite.users.Pikachu, suite.users.Pikachu)

	t.Run("multiple users", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_contributor,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Krosh.UUID.String(),
						},
					},
					{
						Role: pbPub.RepoRole_maintainer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Slowpoke.UUID.String(),
						},
					},
					{
						Role: pbPub.RepoRole_viewer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
					{
						Role: pbPub.RepoRole_developer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})
}

func checkID(
	t *testing.T,
	data []byte,
	users []*entities.User,
) {
	for i, user := range users {
		id, err := yarequire.GetStringFromJSON(data, "subject_roles."+strconv.Itoa(i)+".subject.id")
		require.NoError(t, err)
		require.Equal(t, user.UUID.String(), id)
	}
}

func (suite *RwApiTestSuite) TestPublicAPIRemoveRepoRoles() {
	t := suite.T()

	_, err := suite.gwClient.
		As(suite.users.Admin.Identity).
		SetBody(&pbPub.AddRepoRolesBody{
			SubjectRoles: []*pbPub.SubjectRole{
				{
					Role: pbPub.RepoRole_contributor,
					Subject: &pbPub.Subject{
						Type: pbPub.Subject_user,
						Id:   suite.users.Krosh.UUID.String(),
					},
				},
				{
					Role: pbPub.RepoRole_maintainer,
					Subject: &pbPub.Subject{
						Type: pbPub.Subject_user,
						Id:   suite.users.Slowpoke.UUID.String(),
					},
				},
				{
					Role: pbPub.RepoRole_viewer,
					Subject: &pbPub.Subject{
						Type: pbPub.Subject_user,
						Id:   suite.users.Pikachu.UUID.String(),
					},
				},
				{
					Role: pbPub.RepoRole_developer,
					Subject: &pbPub.Subject{
						Type: pbPub.Subject_user,
						Id:   suite.users.Pikachu.UUID.String(),
					},
				},
			},
		}).
		Post(fmt.Sprintf("/repos/id:%s/roles", suite.repos.Alpha.UUID.String()))

	require.NoError(t, err)

	users := []*entities.User{suite.users.Admin, suite.users.Slowpoke, suite.users.Pikachu, suite.users.Pikachu}

	t.Run("delete", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_contributor,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Krosh.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/%s/%s/roles/remove", suite.repos.Alpha.OrgSlug, suite.repos.Alpha.Slug))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})

	users = users[:len(users)-1]

	t.Run("delete by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_developer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles/remove", suite.repos.Alpha.UUID.String()))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})

	t.Run("access denied", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_viewer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles/remove", suite.repos.Alpha.UUID.String()))
		require.NoError(t, err)

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id")
	})

	users = users[0:1]

	t.Run("multiple users", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.AddRepoRolesBody{
				SubjectRoles: []*pbPub.SubjectRole{
					{
						Role: pbPub.RepoRole_contributor,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Krosh.UUID.String(),
						},
					},
					{
						Role: pbPub.RepoRole_maintainer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Slowpoke.UUID.String(),
						},
					},
					{
						Role: pbPub.RepoRole_viewer,
						Subject: &pbPub.Subject{
							Type: pbPub.Subject_user,
							Id:   suite.users.Pikachu.UUID.String(),
						},
					},
				},
			}).
			Post(fmt.Sprintf("/repos/id:%s/roles/remove", suite.repos.Alpha.UUID.String()))
		require.NoError(t, err)

		//yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/id")

		checkID(t, resp.Body(), users)
	})
}
