package integrationtests

import (
	"context"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	"testing"

	"common/testutils/yarequire"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIGetProfile() {
	ctx := context.Background()

	user := suite.users.Krosh
	extUser := suite.users.AuthViewer
	pub := suite.users.PinPublic
	priv := suite.users.Kopatych

	pubID := pub.UUID.String()
	pubSlug := pub.Username
	privID := priv.UUID.String()
	privSlug := priv.Username

	err := suite.UserRepo.UpdateUser(pub.Identity).
		SetLocation("Foo", "Bar").
		SetBio("Foo Bar").
		SetLinks([]*entities.Link{
			{
				Type: entities.LinkTypes.Default,
				Link: "https://ya.ru",
			},
		}).
		SetWorkplace("Yandex", "Developer").
		SetTimezone("Europe/Moscow").
		Commit(ctx)
	require.NoError(suite.T(), err)

	err = suite.UserRepo.UpdateUser(priv.Identity).
		SetLocation("Russia", "Moscow").
		SetBio("bio bio").
		SetLinks([]*entities.Link{
			{
				Type: entities.LinkTypes.Telegram,
				Link: "https://t.me/yandex",
			},
		}).
		SetWorkplace("Yandex Taxi", "Driver, but actually has his own business").
		SetTimezone("Europe/Moscow").
		Commit(ctx)
	require.NoError(suite.T(), err)

	suite.T().Run("get nonexistent profile", func(t *testing.T) {
		resp, err := suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/id:%s", uuid.New().String()))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode())

		resp, err = suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/%s", "nonexistent"))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode())
	})

	suite.T().Run("get public profile (same org)", func(t *testing.T) {
		resp, err := suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/id:%s", pubID))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")

		resp, err = suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/%s", pubSlug))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")
	})

	suite.T().Run("get private profile (same org)", func(t *testing.T) {
		resp, err := suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/id:%s", privID))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")

		resp, err = suite.gwClient.As(user.Identity).Get(fmt.Sprintf("/users/%s", privSlug))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")
	})

	suite.T().Run("get public profile (other org)", func(t *testing.T) {
		resp, err := suite.gwClient.As(extUser.Identity).Get(fmt.Sprintf("/users/id:%s", pubID))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")

		resp, err = suite.gwClient.As(extUser.Identity).Get(fmt.Sprintf("/users/%s", pubSlug))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode())
		// yarequire.HTTPDumpFixture(t, resp)
		yarequire.HTTPCompareWithFixture(t, resp, "**/request_id", "**/id")
	})

	suite.T().Run("get private profile (other org)", func(t *testing.T) {
		resp, err := suite.gwClient.As(extUser.Identity).Get(fmt.Sprintf("/users/id:%s", privID))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode())

		resp, err = suite.gwClient.As(extUser.Identity).Get(fmt.Sprintf("/users/%s", privSlug))
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode())
	})
}
