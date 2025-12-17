package integrationtests

import (
	"common/functools"
	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/entities/billing"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/access"
	"gitcore/internal/services/billing/cloud"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestOrgService() {
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	mkOrg := func() (string, string, error) {
		s := yautils.MustMakeRandomString("slug", 16)

		r, err := c.CreateOrg(ctx, &pb.CreateOrgRequest{
			Slug:        s,
			Description: "Yandex",
			DisplayName: "Yandex",
			Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
		})
		if err != nil {
			return "", "", err
		}

		var metadata pb.CreateOrgMetadata
		err = r.GetMetadata().UnmarshalTo(&metadata)
		if err != nil {
			return "", "", err
		}

		return s, metadata.Id, nil
	}

	suite.T().Run("Create org", func(t *testing.T) {
		r, err := c.CreateOrg(ctx, &pb.CreateOrgRequest{
			Slug:        yautils.MustMakeRandomString("slug", 16),
			Description: "Yandex",
			DisplayName: "Yandex",
			Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
		})

		require.NoError(t, err)

		var metadata pb.CreateOrgMetadata
		var profile pb.OrgProfile
		require.NoError(t, r.GetMetadata().UnmarshalTo(&metadata))
		require.NoError(t, r.GetResponse().UnmarshalTo(&profile))

		require.Equal(t, metadata.Id, profile.Id)
		yarequire.ProtoEqual(t, metadata.OrgIdentity, profile.OrgIdentity)
	})

	suite.T().Run("Personal Org", func(t *testing.T) {
		// personal org has personal org field
		org, err := suite.OrgService.GetPersonalOrganization(ctx, nil, suite.users.Kopatych)
		require.NoError(t, err)

		profile, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: grpc_marshalling.IDInverse(org.ID)},
		})

		require.NoError(t, err)
		require.Equal(t, grpc_marshalling.IDInverse(suite.users.Kopatych.ID), profile.GetPersonalOrg().GetOwnerId())

		// regular org must not have said field
		_, pk, err := mkOrg()
		require.NoError(t, err)
		profile2, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)
		require.Nil(t, profile2.PersonalOrg)
	})

	suite.T().Run("Get org profile", func(t *testing.T) {
		slug, pk, err := mkOrg()
		require.NoError(t, err)
		_, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)

		_, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: slug},
		})
		require.NoError(t, err)

		_, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: "xxxxxx"},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	suite.T().Run("Links validation", func(t *testing.T) {
		_, id, err := mkOrg()
		require.NoError(t, err)

		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id: id,
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
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id: id,
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

	suite.T().Run("Update images", func(t *testing.T) {
		_, pk, err := mkOrg()
		require.NoError(t, err)

		org, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)
		require.Equal(t, "", org.GetLogo().GetUrl())
		require.Equal(t, "", org.GetBackgroundImage().GetUrl())
		// empty right after creation

		upload1 := suite.uploadPic(testutils.UserIdentities.Kopatych)
		upload2 := suite.uploadPic(testutils.UserIdentities.Kopatych)

		_, err = c.UpdateImage(ctx, testutils.FieldMask(&pb.UpdateOrgImageRequest{
			Id:        pk,
			UploadKey: upload1.Key,
			Image:     pb.UpdateOrgImageRequest_LOGO,
		}))
		require.NoError(t, err)

		_, err = c.UpdateImage(ctx, testutils.FieldMask(&pb.UpdateOrgImageRequest{
			Id:        pk,
			UploadKey: upload2.Key,
			Image:     pb.UpdateOrgImageRequest_BACKGROUND,
		}))
		require.NoError(t, err)

		org, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)
		require.NotEqual(t, "", org.GetLogo().GetUrl())
		require.NotEqual(t, "", org.GetBackgroundImage().GetUrl())
	})

	suite.T().Run("Update images attachments", func(t *testing.T) {
		_, pk, err := mkOrg()
		require.NoError(t, err)

		org, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)
		require.Equal(t, "", org.GetLogo().GetUrl())
		require.Equal(t, "", org.GetBackgroundImage().GetUrl())

		logo := suite.UploadAttachment(testutils.UserIdentities.Kopatych, "img.png", entities.AttachmentScopes.OrgLogo)
		background := suite.UploadAttachment(testutils.UserIdentities.Kopatych, "bigimage.jpg", entities.AttachmentScopes.OrgBackground)

		_, err = c.UpdateImageAttachment(ctx, testutils.FieldMask(&pb.UpdateOrgImageAttachmentRequest{
			Id:        pk,
			AttachId:  logo.ID,
			ImageType: pb.UpdateOrgImageAttachmentRequest_IMAGE_TYPE_LOGO,
		}))
		require.NoError(t, err)

		_, err = c.UpdateImageAttachment(ctx, testutils.FieldMask(&pb.UpdateOrgImageAttachmentRequest{
			Id:        pk,
			AttachId:  background.ID,
			ImageType: pb.UpdateOrgImageAttachmentRequest_IMAGE_TYPE_BACKGROUND,
		}))
		require.NoError(t, err)

		org, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Id{Id: pk},
		})
		require.NoError(t, err)
		require.NotEqual(t, "", org.GetLogo().GetUrl())
		require.NotEqual(t, "", org.GetBackgroundImage().GetUrl())
	})

	suite.T().Run("Change slug", func(t *testing.T) {
		_, pkA, err := mkOrg()
		require.NoError(t, err)
		slugB, _, err := mkOrg()
		require.NoError(t, err)
		tmpSlug := yautils.MustMakeRandomString("yy", 16)

		// clash of new slug
		_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
			Id:   pkA,
			Slug: slugB,
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)

		_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
			Id:   pkA,
			Slug: tmpSlug,
		})
		require.NoError(t, err)

		p, err := grpc_marshalling.IDDirect(pkA)
		require.NoError(t, err)

		org, err := suite.OrgRepo.GetOrganizationByID(ctx, p)
		require.NoError(t, err)
		require.Equal(t, tmpSlug, org.Slug)
	})

	suite.T().Run("Fill profile", func(t *testing.T) {
		slug, id, err := mkOrg()
		require.NoError(t, err)

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

		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id:                       id,
			DisplayName:              "azazaz",
			Location:                 loc,
			Links:                    links,
			Description:              "Mega-corporation",
			GenerativeBackgroundSeed: "seeeeeed",
		}))

		require.NoError(t, err)

		r, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: slug},
		})
		require.NoError(t, err)
		require.Equal(t, "azazaz", r.DisplayName)
		require.Equal(t, "seeeeeed", r.GenerativeBackgroundSeed)
		yarequire.ProtoEqual(t, loc, r.GetLocation())
		yarequire.ProtoEqualList(t, links, r.Links)

		// unset links
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id:    id,
			Links: []*pb.Link{},
		}, "links"))

		require.NoError(t, err)

		r, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: slug},
		})
		require.NoError(t, err)
		yarequire.ProtoEqualList(t, []*pb.Link{}, r.Links)

		// unset public name
		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id:          id,
			DisplayName: "",
		}, "display_name"))
		require.NoError(t, err)

		r, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: slug},
		})
		require.NoError(t, err)
		require.Equal(t, "Yandex", r.DisplayName)
	})

	suite.T().Run("Change visibility", func(t *testing.T) {
		slug, orgIDstr, err := mkOrg()
		require.NoError(t, err)
		orgID, err := grpc_marshalling.IDDirect(orgIDstr)
		require.NoError(t, err)
		org, err := suite.OrgService.GetOrganizationByID(ctx, access.NullAuthenticator, orgID)
		require.NoError(t, err)

		user := suite.users.Admin
		require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
			{Subject: user.Subject(), Object: org.Object(), Role: iam.Roles.RepositoriesMaintainer},
		}))

		alpha := suite.ImportRepo(org, "alpha", testutils.BasicRepo, nil)
		alphaIDstr := grpc_marshalling.IDInverse(alpha.ID)

		af := suite.forkRepo(t, user, alphaIDstr)
		af1 := suite.forkRepoToOrg(t, user, alphaIDstr, orgIDstr)
		af2 := suite.forkRepoToOrg(t, user, af1.Id, orgIDstr)
		afp := suite.forkRepo(t, user, af1.Id)
		afp1 := suite.forkRepoToOrg(t, user, afp.Id, orgIDstr)

		// Yandex42 | alpha -> af1 -> af2   afp1
		//				 |        \         A
		//               V         V      /
		// Personal |    af          afp

		_, err = c.UpdateProfile(ctx, testutils.FieldMask(&pb.UpdateOrgProfileRequest{
			Id:         orgIDstr,
			Visibility: pb.ProfileVisibility_PROFILE_PRIVATE,
		}))
		require.NoError(t, err)

		r, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
			Identifier: &pb.GetOrgProfileRequest_Slug{Slug: slug},
		})
		require.NoError(t, err)
		require.Equal(t, pb.ProfileVisibility_PROFILE_PRIVATE, r.Visibility)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.RebuildForkChainForOrg)

		client := pb.NewRepoServiceClient(suite.grpcClient)
		for _, repoID := range []string{alphaIDstr, af.Id, af1.Id, af2.Id, afp.Id, afp1.Id} {
			repo, err := client.Get(testutils.WithAuthorizedGRPC(ctx, user.Identity),
				&pb.GetRepositoryRequest{Repo: &pb.GetRepositoryRequest_Id{Id: repoID}})
			require.NoError(t, err)
			require.Nil(t, repo.ForkOriginId)
			_, err = suite.clone(t, repo.CloneUrl.Https, user)
			require.NoError(t, err)
		}
	})
}

func (suite *RwApiTestSuite) TestOrgService_UpdateSlug() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	// Create org
	createResponse, err := c.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "slug1",
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var metadata pb.CreateOrgMetadata
	err = createResponse.GetMetadata().UnmarshalTo(&metadata)
	require.NoError(t, err)

	orgID := metadata.Id

	// Update slug first time
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug2",
	})
	require.NoError(t, err)

	// Slug is changed
	getResponse, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug2", getResponse.Slug)

	// Update slug to the previous one
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug1",
	})
	require.NoError(t, err)

	// Slug is changed
	getResponse, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug1", getResponse.Slug)

	// Update slug second time
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug3",
	})
	require.NoError(t, err)

	// Slug is changed
	getResponse, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug3", getResponse.Slug)

	// Update slug third time, failure
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug4",
	})
	require.Error(t, err)

	status := status.Convert(err)
	require.Equal(t, codes.FailedPrecondition, status.Code())
	require.Equal(t, "slug change limit reached: 2", status.Message())

	// Slug is not changed
	getResponse, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug3", getResponse.Slug)

	// Update slug to the first one
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug1",
	})
	require.NoError(t, err)

	// Slug is changed
	getResponse, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug1", getResponse.Slug)
}

func (suite *RwApiTestSuite) TestOrgService_CreateOrgWithReservedSlug() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	// Create org
	createResponse, err := c.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "slug1",
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var metadata pb.CreateOrgMetadata
	err = createResponse.GetMetadata().UnmarshalTo(&metadata)
	require.NoError(t, err)

	orgID := metadata.Id

	// Update slug
	_, err = c.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   orgID,
		Slug: "slug2",
	})
	require.NoError(t, err)

	// Slug is changed
	getResponse, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.Equal(t, "slug2", getResponse.Slug)

	// Create another org with reserved slug, failure
	_, err = c.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "slug1",
		Description: "Yandex2",
		DisplayName: "Yandex2",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.Error(t, err)

	status := status.Convert(err)
	require.Equal(t, codes.FailedPrecondition, status.Code())
	require.Equal(t, "slug \"slug1\" is already occupied", status.Message())
}

func (suite *RwApiTestSuite) TestOrgService_SetFL152Compliant() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	// Create org
	createResponse, err := c.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "slug",
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)

	var metadata pb.CreateOrgMetadata
	err = createResponse.GetMetadata().UnmarshalTo(&metadata)
	require.NoError(t, err)

	orgID := metadata.Id

	// Flag is not set
	getResponse, err := c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.False(t, getResponse.Settings.Fl152Compliant)

	// Cannot set FL-152 for public organization
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: true,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant"},
		},
	})
	require.Error(t, err)
	require.Equal(t, "Failed precondition: Cannot make public organization FL-152 compliant", status.Convert(err).Message())

	// Make private
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:         orgID,
		Visibility: pb.ProfileVisibility_PROFILE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"visibility"},
		},
	})
	require.NoError(t, err)

	// Cannot make organization public and set FL-152
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: true,
		Visibility:     pb.ProfileVisibility_PROFILE_PUBLIC,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant", "visibility"},
		},
	})
	require.Error(t, err)
	require.Equal(t, "Failed precondition: Cannot make public organization FL-152 compliant", status.Convert(err).Message())

	// Make public
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:         orgID,
		Visibility: pb.ProfileVisibility_PROFILE_PUBLIC,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"visibility"},
		},
	})
	require.NoError(t, err)

	// Can make private and set FL-152
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: true,
		Visibility:     pb.ProfileVisibility_PROFILE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant", "visibility"},
		},
	})
	require.NoError(t, err)

	// Cannot make public when FL-152 is set
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:         orgID,
		Visibility: pb.ProfileVisibility_PROFILE_PUBLIC,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"visibility"},
		},
	})
	require.Error(t, err)
	require.Equal(t, "Failed precondition: Cannot make public organization FL-152 compliant", status.Convert(err).Message())

	// Can make public and unset FL-152
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: false,
		Visibility:     pb.ProfileVisibility_PROFILE_PUBLIC,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant", "visibility"},
		},
	})
	require.NoError(t, err)

	// Make private
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:         orgID,
		Visibility: pb.ProfileVisibility_PROFILE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"visibility"},
		},
	})
	require.NoError(t, err)

	// Set flag
	_, err = c.UpdateProfile(ctx, &pb.UpdateOrgProfileRequest{
		Id:             orgID,
		Fl152Compliant: true,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant"},
		},
	})
	require.NoError(t, err)

	// Flag is set
	getResponse, err = c.GetProfile(ctx, &pb.GetOrgProfileRequest{
		Identifier: &pb.GetOrgProfileRequest_Id{Id: orgID},
	})
	require.NoError(t, err)
	require.True(t, getResponse.Settings.Fl152Compliant)

	// List my orgs
	meClient := pb.NewMeServiceClient(suite.grpcClient)
	response, err := meClient.ListOrgs(ctx, &pb.ListOrgsRequest{})
	require.NoError(t, err)
	require.Len(t, response.Orgs, 2)
	org := functools.First(response.Orgs, func(o *pb.OrgProfile) bool { return o.Id == orgID })
	require.NotNil(t, org)
	require.True(t, (*org).Settings.Fl152Compliant)
}

func (suite *RwApiTestSuite) TestOrgService_Tariffs() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewOrgServiceClient(suite.grpcClient)

	suite.addOrgRole(t, suite.users.Slowpoke, suite.orgs.Yandex, iam.Roles.OrganizationManagerAdmin)

	orgID := grpc_marshalling.IDInverse(suite.orgs.Yandex.GetOrgID())

	billingGrantsBackend, ok := suite.billingGrantsBackend.(*cloud.StubBillingBackend)
	require.True(t, ok)

	billingGrantsBackend.SetBillingAccountResponse(&billing.BillingAccountResponse{
		OrganizationID: suite.orgs.Yandex.Identity.ID,
	})

	tariffs, err := c.GetTariffs(
		ctx,
		&pb.GetTariffsRequest{
			Identifier: &pb.GetTariffsRequest_Id{
				Id: orgID,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, tariffs.GetSourcecraft(), pb.SourceCraftTariff_SOURCECRAFT_TARIFF_FREE)
	require.Equal(t, tariffs.GetSecurity(), pb.SecurityTariff_SECURITY_TARIFF_FREE)
	require.Equal(t, tariffs.GetCodeassist(), pb.CodeAssistTariff_CODEASSIST_TARIFF_FREE)

	op, err := c.SetTariffs(
		ctx,
		&pb.SetTariffsRequest{
			Identifier: &pb.SetTariffsRequest_Id{
				Id: orgID,
			},
			Tariff: &pb.SetTariffsRequest_Sourcecraft{
				Sourcecraft: pb.SourceCraftTariff_SOURCECRAFT_TARIFF_PRO,
			},
			BillingAccountId: "test-id",
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, op.GetId())
	require.True(t, op.GetDone())

	tariffs, err = c.GetTariffs(
		ctx,
		&pb.GetTariffsRequest{
			Identifier: &pb.GetTariffsRequest_Id{
				Id: orgID,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, tariffs.GetSourcecraft(), pb.SourceCraftTariff_SOURCECRAFT_TARIFF_PRO)
	require.Equal(t, tariffs.GetSecurity(), pb.SecurityTariff_SECURITY_TARIFF_FREE)
	require.Equal(t, tariffs.GetCodeassist(), pb.CodeAssistTariff_CODEASSIST_TARIFF_FREE)
	require.Less(t, time.Now(), tariffs.GetTrialEndSourcecraft().AsTime())
}
