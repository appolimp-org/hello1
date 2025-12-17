package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestGetDefaultCodeAssistSettings() {
	var err error
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	org, err := suite.OrgRepo.GetOrganizationByID(ctx, *suite.users.Kopatych.PersonalOrgID)
	require.NoError(suite.T(), err)

	settings, err := c.GetMyCodeAssistSettings(ctx, &pb.GetMyCodeAssistSettingsRequest{})
	require.NoError(suite.T(), err)
	yarequire.ProtoEqual(suite.T(), &pb.GetMyCodeAssistSettingsResponse{
		Subject: &pb.Subject{
			Type: pb.Subject_USER,
			Id:   testutils.UserIdentities.Kopatych.ID,
		},
		Settings: &pb.CodeAssistSettings{
			BillingOrgIdInner:    grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
			BillingOrgIdExternal: org.Identity.ID,
			AllowEducation:       true,
			Tariff:               "Free",
		},
	}, settings)
}

func (suite *RwApiTestSuite) TestGetRegisteredCodeAssistSettings() {
	var err error
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	org, err := suite.OrgRepo.GetOrganizationByID(ctx, *suite.users.Kopatych.PersonalOrgID)
	require.NoError(suite.T(), err)

	updatedSettings, err := c.UpdateMyCodeAssistSettings(ctx, &pb.UpdateMyCodeAssistSettingsRequest{
		BillingOrgInner: grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
		AllowEducation:  true,
	})
	require.NoError(suite.T(), err)
	yarequire.ProtoEqual(suite.T(), &pb.CodeAssistSettings{
		BillingOrgIdInner:    grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
		BillingOrgIdExternal: org.Identity.ID,
		AllowEducation:       true,
	}, updatedSettings.GetSettings())

	settings, err := c.GetMyCodeAssistSettings(ctx, &pb.GetMyCodeAssistSettingsRequest{})
	require.NoError(suite.T(), err)
	yarequire.ProtoEqual(suite.T(), &pb.GetMyCodeAssistSettingsResponse{
		Subject: &pb.Subject{
			Type: pb.Subject_USER,
			Id:   testutils.UserIdentities.Kopatych.ID,
		},
		Settings: &pb.CodeAssistSettings{
			BillingOrgIdInner:    grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
			BillingOrgIdExternal: org.Identity.ID,
			AllowEducation:       true,
			Tariff:               "Free",
		},
	}, settings)
}

func (suite *RwApiTestSuite) TestChangeOrg() {
	var err error
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	c := pb.NewMeServiceClient(suite.grpcClient)

	org, err := suite.OrgRepo.GetOrganizationByID(ctx, *suite.users.Kopatych.PersonalOrgID)
	require.NoError(suite.T(), err)

	_, err = c.UpdateMyCodeAssistSettings(ctx, &pb.UpdateMyCodeAssistSettingsRequest{
		BillingOrgInner: grpc_marshalling.IDInverse(suite.orgs.Yandex.GetOrgID()),
	})
	require.Error(suite.T(), err)

	updatedSettings, err := c.UpdateMyCodeAssistSettings(ctx, &pb.UpdateMyCodeAssistSettingsRequest{
		BillingOrgInner: grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
	})
	require.NoError(suite.T(), err)
	yarequire.ProtoEqual(suite.T(), &pb.CodeAssistSettings{
		BillingOrgIdInner:    grpc_marshalling.IDInverse(*suite.users.Kopatych.PersonalOrgID),
		BillingOrgIdExternal: org.Identity.ID,
		AllowEducation:       false,
	}, updatedSettings.GetSettings())

	updatedSettings, err = c.UpdateMyCodeAssistSettings(ctx, &pb.UpdateMyCodeAssistSettingsRequest{
		BillingOrgInner: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.GetOrgID()),
	})
	require.NoError(suite.T(), err)
	yarequire.ProtoEqual(suite.T(), &pb.CodeAssistSettings{
		BillingOrgIdInner:    grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
		BillingOrgIdExternal: suite.orgs.Smeshariki.Identity.ID,
		AllowEducation:       false,
	}, updatedSettings.GetSettings())
}

func (suite *RwApiTestSuite) TestGetAnonymCodeAssistSettings() {
	var err error
	ctx := context.Background()
	c := pb.NewMeServiceClient(suite.grpcClient)

	_, err = c.GetMyCodeAssistSettings(ctx, &pb.GetMyCodeAssistSettingsRequest{})
	require.Error(suite.T(), err)
	st, ok := status.FromError(err)
	require.True(suite.T(), ok)
	require.Equal(suite.T(), codes.Unauthenticated, st.Code())
}
