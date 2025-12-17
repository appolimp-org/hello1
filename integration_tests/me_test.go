package integrationtests

import (
	"common/testutils/assertjson"
	"common/utils"
	"gitcore/internal/config"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"testing"
)

func (suite *RepoApiTestSuite) expectSettingsToBe(val map[string]string) {
	t := suite.T()

	res := schemas.UserSettings{}
	resp, err := suite.client.R().
		SetResult(&res).
		Get("/api/v1/me/settings")

	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())
	require.Equal(t, val, res.Settings)
}

func (suite *RepoApiTestSuite) TestMe() {
	t := suite.T()

	tests := []struct {
		name             string
		user             entities.UserIdentity
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
	}{
		{
			name:         "slowpoke",
			user:         testutils.UserIdentities.Slowpoke,
			expectedCode: 200,
			expectedResponse: `{
				"username":"slowpoke",
				"identity":{
					"id":"slowpoke",
					"src":"iam"
				},
				"claims":"<<PRESENCE>>",
				"flags":["onboarded"],
				"publicName":"Slowpoke",
				"avatar":"https://dev.null/avatars/slowpoke.bmp",
				"background":"",
				"locale":"en",
				"visibility":"private"
			}`,
		},
		{
			name:         "kopatych",
			user:         testutils.UserIdentities.Kopatych,
			expectedCode: 200,
			expectedResponse: `{
				"username":"kopatych",
				"identity":{
					"id":"kopatych",
					"src":"iam"
				},
				"claims":"<<PRESENCE>>",
				"flags":["onboarded"],
				"publicName":"Kopatych",
				"avatar":"https://dev.null/avatars/kopatych.bmp",
				"background":"",
				"locale":"en",
				"visibility":"private"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.As(tt.user).
				SetError(&httpErr).
				Get("/api/v1/me")

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode(), string(resp.Body()))

			if tt.expectedResponse != "" {
				assertjson.MatchExact(t, resp.Body(), tt.expectedResponse)
			}
			if tt.expectedErrCode != "" {
				require.Equal(t, tt.expectedErrCode, httpErr.ErrorCode)
			}
		})
	}
}
func (suite *RepoApiTestSuite) TestChangeUsername() {
	t := suite.T()

	// create user with username "kopatych"
	_, err := suite.client.As(testutils.UserIdentities.Kopatych).
		Get("/api/v1/me")
	require.NoError(t, err)

	tests := []struct {
		name             string
		body             schemas.UpdateUsernameRequest
		expectedCode     int
		expectedResponse string
		expectedErrCode  string
	}{
		{
			name: "ok",
			body: schemas.UpdateUsernameRequest{
				Username: utils.PtrFromValue("updated-username"),
			},
			expectedCode: 200,
			expectedResponse: `{
				"username":"updated-username"
			}`,
		},
		{
			name: "conflict",
			body: schemas.UpdateUsernameRequest{
				Username: utils.PtrFromValue("kopatych"),
			},
			expectedCode:    409,
			expectedErrCode: httperrors.ErrSlugOccupied.ErrorCode,
		},
		{
			name:            "empty",
			body:            schemas.UpdateUsernameRequest{},
			expectedCode:    400,
			expectedErrCode: httperrors.ErrValidationFailed.ErrorCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpErr := httperrors.APIError{}

			resp, err := suite.client.As(testutils.UserIdentities.Krosh).
				SetError(&httpErr).
				SetBody(tt.body).
				Post("/api/v1/me/slug")

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

func (suite *RwApiTestSuite) TestChangeProfileChangeAvatar() {
	t := suite.T()

	before := &schemas.CurrentUserProfile{}

	_, err := suite.client.As(testutils.UserIdentities.Kopatych).SetResult(before).Get("/api/v1/me")
	require.NoError(t, err)

	upload := suite.uploadPic(testutils.UserIdentities.Kopatych)

	result := &schemas.CurrentUserProfile{}

	resp, err := suite.client.As(testutils.UserIdentities.Kopatych).
		SetBody(schemas.UpdateUserProfileRequest{AvatarKey: utils.PtrFromValue(upload.Key)}).
		SetResult(result).Post("/api/v1/me")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))

	require.NotEqual(t, "", result.Avatar)
	require.NotEqual(t, before.Avatar, result.Avatar)
}

func (suite *RwApiTestSuite) TestChangeProfileChangeBackground() {
	t := suite.T()

	before := &schemas.CurrentUserProfile{}

	_, err := suite.client.As(testutils.UserIdentities.Kopatych).SetResult(before).Get("/api/v1/me")
	require.NoError(t, err)

	upload := suite.uploadPic(testutils.UserIdentities.Kopatych)

	result := &schemas.CurrentUserProfile{}

	resp, err := suite.client.As(testutils.UserIdentities.Kopatych).
		SetBody(schemas.UpdateUserProfileRequest{
			BackgroundKey: utils.PtrFromValue(upload.Key),
			Visibility:    &entities.Visibilities.Private,
		}).
		SetResult(result).Post("/api/v1/me")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))

	require.NotEqual(t, "", result.Background)
	require.NotEqual(t, before.Background, result.Background)
}

//func (suite *RwApiTestSuite) TestChangeProfileChangeBio() {
//	t := suite.T()
//
//	result := &schemas.CurrentUserProfile{}
//
//	resp, err := suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Visibility: &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//
//	resp, err = suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Visibility: &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, "Updated user's biography.", result.Bio)
//
//	resp, err = suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Bio:        utils.PtrFromValue(""),
//			Visibility: &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, "", result.Bio)
//}
//
//func (suite *RwApiTestSuite) TestChangeProfileChangeSocialNetworks() {
//	t := suite.T()
//
//	result := &schemas.CurrentUserProfile{}
//	networks := make(map[string]string, 4)
//
//	networks["telegram"] = "https://t.me/one"
//	networks["yandex"] = "https://id.yandex.ru/one"
//	resp, err := suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Profile: &networks,
//			Visibility:     &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, networks, result.Profile)
//
//	networks["skype"] = "skype:user_one"
//	delete(networks, "yandex")
//	result = &schemas.CurrentUserProfile{}
//	resp, err = suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Profile: &networks,
//			Visibility:     &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, networks, result.Profile)
//
//	networks = make(map[string]string, 0)
//	result = &schemas.CurrentUserProfile{}
//	resp, err = suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Profile: &networks,
//			Visibility:     &entities.Visibilities.Private,
//		}).
//		SetResponse(result).Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusOK, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, make(map[string]string, 0), result.Profile)
//
//	longString := "This is 32 letters long string. " // len = 2 ^ 5
//	for i := 5; i < 10; i++ {                        // len from 2 ^ 5 to 2 ^ 10
//		longString = longString + longString
//	} // 1024 length string
//	networks["skype"] = "-" + longString // 1025 chars
//
//	httpErr := &httperrors.APIError{}
//	result = &schemas.CurrentUserProfile{}
//	resp, err = suite.client.As(testutils.UserIdentities.Kopatych).
//		SetBody(schemas.UpdateUserProfileRequest{
//			Profile: &networks,
//			Visibility:     &entities.Visibilities.Private,
//		}).
//		SetResponse(result).
//		SetError(httpErr).
//		Post("/api/v1/me")
//	require.NoError(t, err)
//	require.Equal(t, http.StatusBadRequest, resp.StatusCode(), string(resp.Body()))
//	require.Equal(t, "validation_failed", httpErr.ErrorCode)
//	require.Equal(t, map[string]any{
//		"socialNetworks": map[string]any{
//			"code":    "wrong_value",
//			"message": "skype: the length must be no more than 1024.",
//		},
//	}, httpErr.Details)
//
//}

func (suite *RepoApiTestSuite) TestUserSettings() {
	t := suite.T()

	t.Run("simple write", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.SetSetting{
				Key:   "some_setting_name",
				Value: "some_value",
			}).
			Post("/api/v1/me/settings/set")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		suite.expectSettingsToBe(map[string]string{"some_setting_name": "some_value"})
	})
	t.Run("change record", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.SetSetting{
				Key:   "some_setting_name",
				Value: "other_value",
			}).
			Post("/api/v1/me/settings/set")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		suite.expectSettingsToBe(map[string]string{"some_setting_name": "other_value"})
	})

	t.Run("two records", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.SetSetting{
				Key:   "some_setting_name2",
				Value: "other_value2",
			}).
			Post("/api/v1/me/settings/set")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		suite.expectSettingsToBe(map[string]string{"some_setting_name2": "other_value2",
			"some_setting_name": "other_value",
		})
	})

	t.Run("unset record", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.SetSetting{
				Key: "some_setting_name",
			}).
			Post("/api/v1/me/settings/unset")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		suite.expectSettingsToBe(map[string]string{"some_setting_name2": "other_value2"})
	})

}

func (suite *RepoApiTestSuite) TestFlags() {
	t := suite.T()
	var r schemas.ChangeFlagResponse

	t.Run("simple write", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.ChangeFlag{
				Flag:  "onboarded",
				Value: true,
			}).
			SetResult(&r).
			Post("/api/v1/me/flags")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		require.ElementsMatch(t, []string{"onboarded"}, r.Flags)
	})
	t.Run("idempotency", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.ChangeFlag{
				Flag:  "onboarded",
				Value: true,
			}).
			SetResult(&r).
			Post("/api/v1/me/flags")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		require.ElementsMatch(t, []string{"onboarded"}, r.Flags)
	})

	t.Run("two records", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.ChangeFlag{
				Flag:  "beta",
				Value: true,
			}).
			SetResult(&r).
			Post("/api/v1/me/flags")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		require.ElementsMatch(t, []string{"onboarded", "beta"}, r.Flags)
	})

	t.Run("unset flag", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.ChangeFlag{
				Flag:  "beta",
				Value: false,
			}).
			SetResult(&r).
			Post("/api/v1/me/flags")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode())
		require.ElementsMatch(t, []string{"onboarded"}, r.Flags)
	})

	t.Run("bad flags", func(t *testing.T) {
		resp, err := suite.client.R().
			SetBody(schemas.ChangeFlag{
				Flag:  "wow",
				Value: true,
			}).
			Post("/api/v1/me/flags")
		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
	})

}

func (suite *RwApiTestSuite) uploadPic(identity entities.UserIdentity) *schemas.UploadFileResponse {
	t := suite.T()

	fileData, err := os.ReadFile(testutils.GetAttachmentPath("img.png"))
	require.NoError(t, err)

	result := &schemas.UploadFileResponse{}

	resp, err := suite.client.As(identity).
		SetQueryParams(map[string]string{
			"file_name":   "img.png",
			"upload_type": string(schemas.FileUploadTypes.Image),
		}).
		SetHeaders(map[string]string{
			"Content-Type": "image/png",
		}).
		SetBody(fileData).
		SetResult(&result).
		Post("/api/v1/me/uploads")
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode())

	return result
}

func (suite *RwApiTestSuite) TestUploads() {
	t := suite.T()
	cfg, err := config.GetUnittestAppConfig()
	require.NoError(t, err)

	t.Run("Upload", func(t *testing.T) {
		result := suite.uploadPic(testutils.UserIdentities.Kopatych)
		require.Equal(t, cfg.S3.Attachments.User.TextTTLMins, result.TTLInMinutes)
		require.NotEqual(t, "", result.Key)
	})

	t.Run("Upload file too big", func(t *testing.T) {
		limit := suite.cfg.S3.Attachments.User.MaxFileSizeMB
		defer func() { suite.cfg.S3.Attachments.User.MaxFileSizeMB = limit }()

		suite.cfg.S3.Attachments.User.MaxFileSizeMB = 0

		httpErr := httperrors.APIError{}

		resp, err := suite.client.As(testutils.UserIdentities.Kopatych).
			SetQueryParams(map[string]string{
				"file_name":   "img.png",
				"upload_type": string(schemas.FileUploadTypes.Image),
			}).
			SetHeaders(map[string]string{
				"Content-Type": "image/png",
			}).
			SetError(&httpErr).
			SetBody([]byte("aa")).
			Post("/api/v1/me/uploads")
		require.NoError(t, err)
		require.Equal(t, 400, resp.StatusCode())
		require.Equal(t, httpErr.ErrorCode, except.UploadIsTooBig.MessageID)
	})
}
