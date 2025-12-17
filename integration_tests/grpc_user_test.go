package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestUserService() {
	var err error
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewUserServiceClient(suite.grpcClient)

	err = suite.UserRepo.UpdateUser(testutils.UserIdentities.Barash).
		SetLocation("Foo", "Bar").
		SetVisibility(entities.Visibilities.Private).
		Commit(ctx)
	require.NoError(suite.T(), err)

	err = suite.UserRepo.UpdateUser(testutils.UserIdentities.PinPublic).
		SetLocation("Foo", "Bar").
		Commit(ctx)
	require.NoError(suite.T(), err)

	// create migrated user
	migratedUser, err := suite.UserRepo.CreateUser(ctx, entities.User{
		Username: "123@github",
		Email:    "123@github",
		Identity: entities.UserIdentity{
			ID:  "123",
			Src: entities.IdentityProviders.Migration,
		},
		Visibility:  entities.Visibilities.Public,
		DisplayName: "123",
		Status:      entities.UserStatuses.Active,
	})
	require.NoError(suite.T(), err)

	// public
	suite.T().Run("get public profile", func(t *testing.T) {
		r, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_UserIdentity{UserIdentity: grpc_marshalling.EntityToPB.UserIdentity(&suite.users.PinPublic.Identity)},
		})
		require.NoError(t, err)
		require.Equal(t, "Pinpublic", r.DisplayName)
		require.NotNil(t, r.Location)
	})

	suite.T().Run("personal org", func(t *testing.T) {
		org, err := suite.OrgService.GetPersonalOrganization(ctx, nil, suite.users.Kopatych)
		require.NoError(t, err)

		r, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_UserIdentity{UserIdentity: grpc_marshalling.EntityToPB.UserIdentity(&suite.users.Kopatych.Identity)},
		})

		require.NoError(t, err)
		require.Equal(t, grpc_marshalling.IDInverse(org.ID), r.GetPersonalOrgId())
	})

	suite.T().Run("get public profile by ID", func(t *testing.T) {
		r, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Id{Id: grpc_marshalling.IDInverse(suite.users.PinPublic.ID)},
		})
		require.NoError(t, err)
		require.Equal(t, "Pinpublic", r.DisplayName)
	})

	suite.T().Run("get public profile by slug", func(t *testing.T) {
		r, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Slug{Slug: suite.users.PinPublic.Username},
		})
		require.NoError(t, err)
		require.Equal(t, "Pinpublic", r.DisplayName)
	})

	suite.T().Run("get public profile: not found", func(t *testing.T) {
		_, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Slug{Slug: "this-slug-not-exists"},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	suite.T().Run("get private profile (identical orgs)", func(t *testing.T) {
		_, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_UserIdentity{UserIdentity: grpc_marshalling.EntityToPB.UserIdentity(&suite.users.Barash.Identity)},
		})
		require.NoError(t, err)
	})

	suite.T().Run("get private profile (another org)", func(t *testing.T) {
		_, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_UserIdentity{UserIdentity: grpc_marshalling.EntityToPB.UserIdentity(&suite.users.AuthViewer.Identity)},
		})
		require.Error(t, err)
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	suite.T().Run("get profiles bulk", func(t *testing.T) {
		ids := []uint64{suite.users.Krosh.ID, 999, suite.users.Kopatych.ID, suite.users.Pikachu.ID}

		r, err := c.GetProfiles(ctx, &pb.GetUserProfilesRequest{
			UserIds: functools.Map(ids, grpc_marshalling.IDInverse),
		})
		require.NoError(t, err)
		require.Equal(t, 3, len(r.Profiles))
		require.Equal(t, "Krosh", r.Profiles[0].DisplayName)
		require.Equal(t, "Kopatych", r.Profiles[1].DisplayName)
		require.Equal(t, "Pikachu", r.Profiles[2].DisplayName)
	})

	suite.T().Run("get profiles bulk - iam users and migrated users", func(t *testing.T) {
		ids := []uint64{suite.users.Krosh.ID, 999, migratedUser.ID, suite.users.BiBiPublic.ID}

		resp, err := c.GetProfiles(ctx, &pb.GetUserProfilesRequest{
			UserIds: functools.Map(ids, grpc_marshalling.IDInverse),
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "id", "uuid"),
		)
	})

	suite.T().Run("get profile - migrated user", func(t *testing.T) {
		resp, err := c.GetProfile(ctx, &pb.GetUserProfileRequest{
			Identifier: &pb.GetUserProfileRequest_Id{Id: grpc_marshalling.IDInverse(migratedUser.ID)},
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp,
			protocmp.IgnoreFields(&pb.UserProfile{}, "onboarded_at", "personal_org_id", "id", "uuid"),
		)
	})
}

func parseSettings(t *testing.T, op *operation.Operation) map[string]string {
	response, err := op.GetResponse().UnmarshalNew()
	require.NoError(t, err)
	settings, ok := response.(*pb.UpdateSettingsResponse)
	require.True(t, ok)
	return settings.Settings
}

func (suite *RwApiTestSuite) TestMeService() {
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	suite.T().Run("get my profile", func(t *testing.T) {
		r, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, "Kopatych", r.Profile.DisplayName)
		require.Equal(t, "kopatych", r.Profile.Username)
	})

	suite.T().Run("UI locale", func(t *testing.T) {
		r, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.Locale_LC_EN_US, r.UiLocale)
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			UiLocale: pb.Locale_LC_RU_RU,
		}))
		require.NoError(t, err)

		r, err = c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, pb.Locale_LC_RU_RU, r.UiLocale)
	})

	suite.T().Run("flags modification", func(t *testing.T) {
		idx := entities.UserIdentity{
			ID:  "new-user",
			Src: entities.IdentityProviders.IAM,
		}

		suite.usersPk++
		_, err := suite.UserService.CreateUser(context.Background(), testutils.NewStubAuthenticator(&idx), interfaces.UserCreateArgs{
			ID:       suite.usersPk,
			Identity: idx,
		}, false)
		require.NoError(t, err)

		ctxNewUser := testutils.AuthorizeGRPC(idx)

		r, err := c.GetProfile(ctxNewUser, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, 0, len(r.Flags))
		require.Nil(t, r.Profile.OnboardedAt)

		_, err = c.SetFlag(ctxNewUser, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_ONBOARDED})
		require.NoError(t, err)

		_, err = suite.UserRepo.SetFlag(ctxNewUser, suite.users.Kopatych.ID, entities.UserFlags.Onboarded, true)
		require.NoError(t, err)

		r, err = c.GetProfile(ctxNewUser, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, []pb.UserFlag{pb.UserFlag_FLAG_ONBOARDED}, r.Flags)
		require.NotNil(t, r.Profile.OnboardedAt)
	})

	suite.T().Run("timezone validation", func(t *testing.T) {
		_, err := c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Timezone: &pb.Timezone{
				IanaTimezone: "FOO/BAR",
			},
		}))
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Timezone: &pb.Timezone{
				IanaTimezone: "Europe/Moscow",
			},
		}))
		require.NoError(t, err)
	})

	suite.T().Run("field mask validation", func(t *testing.T) {
		fm, _ := fieldmaskpb.New(&pb.UpdateProfileRequest{})
		fm.Paths = []string{"this_field_doest_not_exist"}

		_, err := c.UpdateProfile(ctx, &pb.UpdateProfileRequest{
			DisplayName: "azazaz",
			UpdateMask:  fm,
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
	})

	suite.T().Run("enum validation x field mask", func(t *testing.T) {
		// field mask includes field, despite it is empty -- will be checked
		_, err := c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Visibility: pb.ProfileVisibility_PROFILE_UNSPECIFIED,
			Bio:        "insect",
		}, "visibility"))

		yarequire.ProtoStatusInvalidArgument(t, err, "visibility", "NotIn")

		// no fieldmask = validate every field
		_, err = c.UpdateProfile(ctx, &pb.UpdateProfileRequest{
			Visibility: pb.ProfileVisibility_PROFILE_UNSPECIFIED,
			Bio:        "insect",
		})
		yarequire.ProtoStatusInvalidArgument(t, err, "visibility", "NotIn")

		// pb.ProfileVisibility_PROFILE_UNSPECIFIED = 0
		// and if field mask does not include it, it will not be checked
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Visibility: pb.ProfileVisibility_PROFILE_UNSPECIFIED,
			Bio:        "insect",
		}))

		require.NoError(t, err)
	})

	suite.T().Run("links validation", func(t *testing.T) {
		_, err := c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Links: []*pb.Link{
				{
					Link: "aa@ya.ru",
					Type: pb.Link_HOMEPAGE,
				},
				{
					Link: "aa@ya.ru",
					Type: pb.Link_EMAIL,
				},
			},
		}))
		yarequire.ProtoStatusInvalidArgument(t, err, "links[0]", "URI")

		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Links: []*pb.Link{
				{
					Link: "http://ya.ru",
					Type: pb.Link_HOMEPAGE,
				},
				{
					Link: "http://ya.ru",
					Type: pb.Link_EMAIL,
				},
			},
		}))
		yarequire.ProtoStatusInvalidArgument(t, err, "links[1]", "Email")
	})

	suite.T().Run("update images", func(t *testing.T) {
		profileBefore, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)

		upload1 := suite.uploadPic(testutils.UserIdentities.Kopatych)
		upload2 := suite.uploadPic(testutils.UserIdentities.Kopatych)

		_, err = c.UpdateImage(ctx, testutils.FieldMask(&pb.UpdateImageRequest{
			UploadKey: upload1.Key,
			Image:     pb.UpdateImageRequest_BACKGROUND,
		}))
		require.NoError(t, err)

		_, err = c.UpdateImage(ctx, testutils.FieldMask(&pb.UpdateImageRequest{
			UploadKey: upload2.Key,
			Image:     pb.UpdateImageRequest_AVATAR,
		}))
		require.NoError(t, err)

		profileAfter, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.NotEqual(t, profileAfter.Profile.GetAvatar().GetUrl(), profileBefore.Profile.GetAvatar().GetUrl())
		require.NotEqual(t, profileAfter.Profile.GetBackgroundImage().GetUrl(), profileBefore.Profile.GetBackgroundImage().GetUrl())
	})

	suite.T().Run("update images attachments", func(t *testing.T) {
		profileBefore, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)

		avatar := suite.UploadAttachment(testutils.UserIdentities.Kopatych, "img.png", entities.AttachmentScopes.UserAvatar)
		background := suite.UploadAttachment(testutils.UserIdentities.Kopatych, "bigimage.jpg", entities.AttachmentScopes.UserBackground)

		_, err = c.UpdateImageAttachment(ctx, testutils.FieldMask(&pb.UpdateImageAttachmentRequest{
			AttachId:  avatar.ID,
			ImageType: pb.UpdateImageAttachmentRequest_IMAGE_TYPE_AVATAR,
		}))
		require.NoError(t, err)

		_, err = c.UpdateImageAttachment(ctx, testutils.FieldMask(&pb.UpdateImageAttachmentRequest{
			AttachId:  background.ID,
			ImageType: pb.UpdateImageAttachmentRequest_IMAGE_TYPE_BACKGROUND,
		}))
		require.NoError(t, err)

		profileAfter, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.NotEqual(t, profileAfter.Profile.GetAvatar().GetUrl(), profileBefore.Profile.GetAvatar().GetUrl())
		require.NotEqual(t, profileAfter.Profile.GetBackgroundImage().GetUrl(), profileBefore.Profile.GetBackgroundImage().GetUrl())
	})

	suite.T().Run("change slug", func(t *testing.T) {
		// clash of new slug
		_, err := c.UpdateUsername(ctx, &pb.UpdateUsernameRequest{
			Username: "pikachu",
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
		_, err = c.UpdateUsername(ctx, &pb.UpdateUsernameRequest{
			Username: "xxx",
		})
		require.NoError(t, err)

		r, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, "xxx", r.Profile.Username)

		_, err = c.UpdateUsername(ctx, &pb.UpdateUsernameRequest{
			Username: "kopatych",
		})
		require.NoError(t, err)

	})
	suite.T().Run("fill profile", func(t *testing.T) {
		links := []*pb.Link{
			{
				Link: "https://ya.ru",
				Type: pb.Link_DEFAULT,
			},
			{
				Link: "https://src.yandex.ru",
				Type: pb.Link_HOMEPAGE,
			},
		}
		loc := &pb.Location{
			Country: "Moon",
			City:    "Mare Tranquillitatis",
		}
		tz := &pb.Timezone{
			IanaTimezone: "Europe/Moscow",
		}
		wp := &pb.Workplace{
			Company:  "Yandex",
			Position: "Developer",
		}

		_, err := c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			DisplayName:              "azazaz",
			Location:                 loc,
			Timezone:                 tz,
			Workplace:                wp,
			Links:                    links,
			Bio:                      "Professional helloworlder",
			GenerativeBackgroundSeed: "seeeeed",
		}))

		require.NoError(t, err)

		r, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, "azazaz", r.Profile.DisplayName)
		require.Equal(t, "seeeeed", r.Profile.GenerativeBackgroundSeed)

		yarequire.ProtoEqual(t, wp, r.Profile.GetWorkplace())
		yarequire.ProtoEqual(t, tz, r.Profile.GetTimezone())
		yarequire.ProtoEqual(t, loc, r.Profile.GetLocation())
		yarequire.ProtoEqualList(t, links, r.Profile.Links)

		// unset links
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			Links: []*pb.Link{},
		}, "links"))
		require.NoError(t, err)

		r, err = c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
		require.Equal(t, "azazaz", r.Profile.DisplayName) // FieldMask prevents unsetting of DisplayName
		yarequire.ProtoEqualList(t, []*pb.Link{}, r.Profile.Links)

		// unset public name fails
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateProfileRequest{
			DisplayName: "",
		}, "display_name"))
		require.Error(t, err)

	})

	suite.T().Run("update settings", func(t *testing.T) {
		// 0 keys
		r, err := c.GetSettings(ctx, &pb.GetSettingsRequest{})
		require.NoError(t, err)
		require.Equal(t, map[string]string(nil), r.Settings)

		// 1 key
		r2, err := c.UpdateSettings(ctx, &pb.UpdateSettingsRequest{
			Action: pb.DeltaAction_ADD,
			Key:    "key1",
			Value:  proto.String("value1"),
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"key1": "value1"}, parseSettings(t, r2))

		// 2 keys
		r3, err := c.UpdateSettings(ctx, &pb.UpdateSettingsRequest{
			Action: pb.DeltaAction_ADD,
			Key:    "key2",
			Value:  proto.String("value2"),
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"key1": "value1", "key2": "value2"}, parseSettings(t, r3))

		r4, err := c.UpdateSettings(ctx, &pb.UpdateSettingsRequest{
			Action: pb.DeltaAction_ADD,
			Key:    "key1",
			Value:  proto.String("value3"),
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"key1": "value3", "key2": "value2"}, parseSettings(t, r4))

		r5, err := c.GetSettings(ctx, &pb.GetSettingsRequest{})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"key1": "value3", "key2": "value2"}, r5.Settings)

		// 1 key
		r6, err := c.UpdateSettings(ctx, &pb.UpdateSettingsRequest{
			Action: pb.DeltaAction_REMOVE,
			Key:    "key1",
			Value:  proto.String("unused"),
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"key2": "value2"}, parseSettings(t, r6))

		// 0 keys
		r7, err := c.UpdateSettings(ctx, &pb.UpdateSettingsRequest{
			Action: pb.DeltaAction_REMOVE,
			Key:    "key2",
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string(nil), parseSettings(t, r7))

		r8, err := c.GetSettings(ctx, &pb.GetSettingsRequest{})
		require.NoError(t, err)
		require.Equal(t, map[string]string(nil), r8.Settings)
	})
}

func (suite *RwApiTestSuite) TestUserFlags() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	t.Run("simple write", func(t *testing.T) {
		r, err := c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_ONBOARDED, Value: true})
		require.NoError(t, err)

		resp, err := grpc_marshalling.OperationResponse(r, &pb.SetFlagResponse{})
		require.NoError(t, err)
		require.ElementsMatch(t, []pb.UserFlag{pb.UserFlag_FLAG_ONBOARDED}, resp.Flags)
	})
	t.Run("idempotency", func(t *testing.T) {
		r, err := c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_ONBOARDED, Value: true})
		require.NoError(t, err)

		resp, err := grpc_marshalling.OperationResponse(r, &pb.SetFlagResponse{})
		require.NoError(t, err)
		require.ElementsMatch(t, []pb.UserFlag{pb.UserFlag_FLAG_ONBOARDED}, resp.Flags)
	})

	t.Run("two records", func(t *testing.T) {
		r, err := c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_BETA, Value: true})
		require.NoError(t, err)

		resp, err := grpc_marshalling.OperationResponse(r, &pb.SetFlagResponse{})
		require.NoError(t, err)
		require.ElementsMatch(t, []pb.UserFlag{pb.UserFlag_FLAG_ONBOARDED, pb.UserFlag_FLAG_BETA}, resp.Flags)
	})

	t.Run("unset flag", func(t *testing.T) {
		r, err := c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_BETA, Value: false})
		require.NoError(t, err)

		resp, err := grpc_marshalling.OperationResponse(r, &pb.SetFlagResponse{})
		require.NoError(t, err)
		require.ElementsMatch(t, []pb.UserFlag{pb.UserFlag_FLAG_ONBOARDED}, resp.Flags)
	})
}

func (suite *RwApiTestSuite) TestMeService_ListOrgs() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	resp, err := c.ListOrgs(ctx, &pb.ListOrgsRequest{})
	require.NoError(t, err)

	//yarequire.ProtoDumpFixture(t, resp)
	yarequire.ProtoCompareWithFixture(t, resp,
		protocmp.IgnoreFields(&pb.OrgProfile{}, "id", "revision"),
		protocmp.IgnoreFields(&pb.OrgIdentity{}, "id"),
	)
}

func (suite *RwApiTestSuite) TestMeService_CanUseAI() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	resp, err := c.CanUseAI(ctx, &pb.CanUseAIRequest{})
	require.NoError(t, err)
	require.True(t, resp.Verdict)

	orgClient := pb.NewOrgServiceClient(suite.grpcClient)
	createResponse, err := orgClient.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "test",
		Description: "Test",
		DisplayName: "Test",
		Visibility:  pb.ProfileVisibility_PROFILE_PRIVATE,
	})
	require.NoError(t, err)

	var metadata pb.CreateOrgMetadata
	err = createResponse.GetMetadata().UnmarshalTo(&metadata)
	require.NoError(t, err)

	orgID := metadata.Id

	_, err = orgClient.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: true,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant"},
		},
	})
	require.NoError(t, err)

	resp, err = c.CanUseAI(ctx, &pb.CanUseAIRequest{})
	require.NoError(t, err)
	require.False(t, resp.Verdict)
}

func (suite *RwApiTestSuite) TestCreateUser_EmptyEmail() {
	t := suite.T()

	t.Run("first user", func(t *testing.T) {
		userIdentity := entities.UserIdentity{
			ID:  "sovunya",
			Src: entities.IdentityProviders.IAM,
		}
		suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
			PublicName:        "Sovunya",
			PreferredUsername: "sovunya",
			Email:             "",
		})
		ctx := testutils.AuthorizeGRPC(userIdentity)

		c := pb.NewMeServiceClient(suite.grpcClient)

		_, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)

		// test indexation
		_, err = c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_ONBOARDED, Value: true})
		require.NoError(t, err)
	})
	t.Run("second user", func(t *testing.T) {
		userIdentity := entities.UserIdentity{
			ID:  "sovunya2",
			Src: entities.IdentityProviders.IAM,
		}
		suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
			PublicName:        "Sovunya",
			PreferredUsername: "sovunya",
			Email:             "",
		})
		ctx := testutils.AuthorizeGRPC(userIdentity)

		c := pb.NewMeServiceClient(suite.grpcClient)

		_, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)

		// test indexation
		_, err = c.SetFlag(ctx, &pb.SetFlagRequest{Flag: pb.UserFlag_FLAG_ONBOARDED, Value: true})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestCreateUser_DuplicateEmail() {
	t := suite.T()

	t.Run("first user", func(t *testing.T) {
		userIdentity := entities.UserIdentity{
			ID:  "sovunya",
			Src: entities.IdentityProviders.IAM,
		}
		suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
			PublicName:        "Sovunya",
			PreferredUsername: "sovunya",
			Email:             "sovunya@ya.ru",
		})
		ctx := testutils.AuthorizeGRPC(userIdentity)

		c := pb.NewMeServiceClient(suite.grpcClient)

		_, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
	})
	t.Run("second user", func(t *testing.T) {
		userIdentity := entities.UserIdentity{
			ID:  "sovunya2",
			Src: entities.IdentityProviders.IAM,
		}
		suite.StubClaimService.SetClaimsForUnittestUser(userIdentity, entities.Claims{
			PublicName:        "Sovunya",
			PreferredUsername: "sovunya",
			Email:             "sovunya@ya.ru",
		})
		ctx := testutils.AuthorizeGRPC(userIdentity)

		c := pb.NewMeServiceClient(suite.grpcClient)

		_, err := c.GetProfile(ctx, &pb.GetMyProfileRequest{})
		require.NoError(t, err)
	})
}
