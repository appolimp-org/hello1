package integrationtests

import (
	"context"
	"testing"

	"common/testutils/yarequire"
	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/services/access"
	userservice "gitcore/internal/services/user"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"private_api/generated/yandex/cloud/priv/saas"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgServiceCreate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewOrgServiceClient(suite.grpcClient)

	federativeUser := suite.UserFixture(entities.UserIdentity{
		ID:  "federal1",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)
	suite.addOrgRole(t, federativeUser, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)

	tt := map[string]struct {
		user                              *entities.User
		request                           *pb.CreateOrgRequest
		expectedStatus                    codes.Code
		verifyEventMetaDataOrganizationID bool
	}{
		"org created": {
			user: suite.users.Admin,
			request: &pb.CreateOrgRequest{
				Slug:        yautils.MustMakeRandomString("slug", 16),
				Description: "Yandex",
				DisplayName: "Yandex",
				Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
			},
		},
		"org create error": {
			user: federativeUser,
			request: &pb.CreateOrgRequest{
				Slug:        yautils.MustMakeRandomString("slug", 16),
				Description: "Yandex",
				DisplayName: "Yandex",
				Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
			},
			expectedStatus:                    codes.PermissionDenied,
			verifyEventMetaDataOrganizationID: false,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.CreateOrg(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetOnboardOrganizationAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			if tc.verifyEventMetaDataOrganizationID {
				et.HasEventMetaDataOrganizationID(t, msgs[0])
			}
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgServiceUpdate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewOrgServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	resp, err := client.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)
	var profile pb.OrgProfile
	require.NoError(t, resp.GetResponse().UnmarshalTo(&profile))

	federativeUser := suite.UserFixture(entities.UserIdentity{
		ID:  "federal1",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)
	suite.addOrgRole(t, federativeUser, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdateOrgProfileRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"org updated": {
			user: suite.users.Admin,
			request: &pb.UpdateOrgProfileRequest{
				Id:                       profile.Id,
				DisplayName:              "NewTestDisplayName1",
				Description:              "NewTestDescription1",
				Visibility:               pb.ProfileVisibility_PROFILE_PRIVATE,
				GenerativeBackgroundSeed: "seed",
				UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"display_name", "description", "visibility", "generative_background_seed"}},
			},
		},
		"org update permission error": {
			user: suite.users.Krosh,
			request: &pb.UpdateOrgProfileRequest{
				Id:                       profile.Id,
				DisplayName:              "NewTestDisplayName2",
				Description:              "NewTestDescription2",
				Visibility:               pb.ProfileVisibility_PROFILE_PRIVATE,
				GenerativeBackgroundSeed: "seed2",
				UpdateMask:               &fieldmaskpb.FieldMask{Paths: []string{"display_name", "description", "visibility", "generative_background_seed"}},
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.UpdateProfile(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdateOrganizationAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgServiceUpdateSlug() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewOrgServiceClient(suite.grpcClient)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	resp, err := client.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, err)
	var profile pb.OrgProfile
	require.NoError(t, resp.GetResponse().UnmarshalTo(&profile))

	federativeUser := suite.UserFixture(entities.UserIdentity{
		ID:  "federal1",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)
	suite.addOrgRole(t, federativeUser, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdateOrgSlugRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"org slug updated": {
			user: suite.users.Admin,
			request: &pb.UpdateOrgSlugRequest{
				Id:   profile.Id,
				Slug: "newslug1",
			},
		},
		"org slug update permission error": {
			user: suite.users.Krosh,
			request: &pb.UpdateOrgSlugRequest{
				Id:   profile.Id,
				Slug: "newslug2",
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.UpdateSlug(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdateOrganizationAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgOffboard() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	client := pb.NewOrgServiceClient(suite.grpcClient)
	createResp, createErr := client.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        yautils.MustMakeRandomString("slug", 16),
		Description: "Yandex",
		DisplayName: "Yandex",
		Visibility:  pb.ProfileVisibility_PROFILE_PUBLIC,
	})
	require.NoError(t, createErr)
	var profile pb.OrgProfile
	require.NoError(t, createResp.GetResponse().UnmarshalTo(&profile))

	orgID, err2 := grpc_marshalling.IDDirect(profile.Id)
	require.NoError(t, err2)
	org, getOrgErr := suite.OrgService.GetOrganizationByID(ctx, nil, orgID)
	require.NoError(t, getOrgErr)

	err := suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{{
		Subject: suite.users.Admin.Subject(),
		Object: entities.IAMObject{
			Type: entities.ObjectTypes.Organization,
			ID:   org.ID,
		},
		Role: iam.Roles.InternalOrganizationManagerReaperAgent,
	}})
	require.NoError(t, err)

	require.NoError(t, et.DeleteAuditEvents(ctx))

	instanceClient := saas.NewInstanceServiceClient(suite.grpcClient)
	operationClient := pb.NewOperationServiceClient(suite.grpcClient)

	resp, err := instanceClient.Delete(ctx, &saas.DeleteInstanceRequest{
		OrganizationId: org.Identity.ID, ServiceId: "src",
	})
	require.NoError(t, err)
	require.False(t, resp.Done)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.DeleteInstance)

	op, err := operationClient.Get(ctx, &pb.GetOperationRequest{Id: resp.Id})
	require.NoError(t, err)
	require.True(t, op.Done)

	msgs, err := et.GetOffboardOrganizationAuditEvents(ctx)
	require.NoError(t, err)
	require.True(t, len(msgs) > 0)

	t.Run("started", func(t *testing.T) {
		//yarequire.ProtoDumpFixture(t, msgs[0])
		yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

		et.HasEventMetaDataOrganizationID(t, msgs[0])
	})
	t.Run("done", func(t *testing.T) {
		//yarequire.ProtoDumpFixture(t, msgs[1])
		yarequire.ProtoCompareWithFixture(t, msgs[1], et.ProtoCompareOpts()...)

		et.HasEventMetaDataOrganizationID(t, msgs[0])
	})
}

func (suite *RwApiTestSuite) TestNoAuditEventForGrpcOrgOffboardWhenOrgNotFound() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	require.NoError(t, et.DeleteAuditEvents(ctx))

	instanceClient := saas.NewInstanceServiceClient(suite.grpcClient)

	// Try to offboard a non-existent organization
	nonExistentOrgID := "non-existent-org-id-12345"
	_, err := instanceClient.Delete(ctx, &saas.DeleteInstanceRequest{
		OrganizationId: nonExistentOrgID,
		ServiceId:      "src",
	})

	// Expect an error (organization not found)
	require.Error(t, err)

	// Verify NO audit events were created for the failed offboard attempt
	msgs, err := et.GetOffboardOrganizationAuditEvents(ctx)
	require.NoError(t, err)
	require.Empty(t, msgs, "No audit events should be created when organization is not found")
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgServiceCreateOnGetProfile() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewMeServiceClient(suite.grpcClient)

	newUser := &entities.UserIdentity{
		ID:  "new_user123_me_service_test_sign_up",
		Src: entities.IdentityProviders.IAM,
	}

	tt := map[string]struct {
		userIdentity                      *entities.UserIdentity
		request                           *pb.GetMyProfileRequest
		expectedStatus                    codes.Code
		verifyEventMetaDataOrganizationID bool
	}{
		"org created on get profile": {
			userIdentity: newUser,
			request:      &pb.GetMyProfileRequest{},
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(*tc.userIdentity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.GetProfile(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			suite.WaitForWorkflows(t, userservice.CreatePersonalOrgWorkflowType)

			msgs, err := et.GetOnboardOrganizationAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			if tc.verifyEventMetaDataOrganizationID {
				et.HasEventMetaDataOrganizationID(t, msgs[0])
			}
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcOrgServiceCreateOnChoosePersonalOrg() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	newUser := &entities.UserIdentity{
		ID:  "new_user124_me_service_test_sign_up",
		Src: entities.IdentityProviders.IAM,
	}

	client := pb.NewMeServiceClient(suite.grpcClient)

	tt := map[string]struct {
		userIdentity                      *entities.UserIdentity
		request                           *pb.ChoosePersonalOrgRequest
		expectedStatus                    codes.Code
		verifyEventMetaDataOrganizationID bool
	}{
		"org created on choose personal org": {
			userIdentity: newUser,
			request: &pb.ChoosePersonalOrgRequest{
				Decision: &pb.ChoosePersonalOrgRequest_CreateNew{
					CreateNew: true,
				},
			},
		},
	}

	suite.usersPk++
	_, err := suite.UserService.CreateUser(context.Background(), testutils.NewStubAuthenticator(newUser), interfaces.UserCreateArgs{
		ID:       suite.usersPk,
		Identity: *newUser,
	}, false)
	require.NoError(t, err)

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(*tc.userIdentity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.ChoosePersonalOrg(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)
			suite.WaitForWorkflows(t, userservice.CreatePersonalOrgWorkflowType)

			msgs, err := et.GetOnboardOrganizationAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			if tc.verifyEventMetaDataOrganizationID {
				et.HasEventMetaDataOrganizationID(t, msgs[0])
			}
		})
	}
}
