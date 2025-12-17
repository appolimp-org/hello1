package integrationtests

import (
	"common/testutils/yarequire"
	"gitcore/internal/entities"
	"gitcore/internal/generated/mocks"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (suite *RepoApiTestSuite) TestGetInviteByCodeGRPC() {
	t := suite.T()
	client := pb.NewInviteServiceClient(suite.grpcClient)
	org := suite.orgs.Yandex42
	repo := suite.repos.Alpha

	cases := []struct {
		name      string
		user      *entities.User
		request   *pb.GetInviteByCodeRequest
		setupMock func(cloudMock *mocks.MockYCPSDKInvitationClient)
		wantCode  codes.Code
	}{
		{
			name: "org_invite",
			user: suite.users.Admin,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.InvitationByCode{
						Invitation: &organizationmanager.Invitation{
							Id:               "invite-org-id",
							Status:           organizationmanager.Invitation_PENDING,
							ServiceUri:       "https://sourcecraft.dev/" + org.Slug,
							InviterSubjectId: suite.users.Barash.Identity.ID,
						},
						OrganizationId: org.Identity.ID,
					}, nil)
			},
		},
		{
			name: "repo_invite",
			user: suite.users.Kopatych,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.InvitationByCode{
						Invitation: &organizationmanager.Invitation{
							Id:               "invite-repo-id",
							Status:           organizationmanager.Invitation_PENDING,
							ServiceUri:       "https://sourcecraft.dev/" + repo.FullSlug(),
							InviterSubjectId: suite.users.Pikachu.Identity.ID,
						},
						OrganizationId: org.Identity.ID,
					}, nil)
			},
		},
		{
			name: "slug_changed",
			user: suite.users.Kopatych,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.InvitationByCode{
						Invitation: &organizationmanager.Invitation{
							Id:               "invite-anon-id",
							Status:           organizationmanager.Invitation_PENDING,
							ServiceUri:       "https://sourcecraft.dev/" + "some_slug",
							InviterSubjectId: suite.users.Barash.Identity.ID,
						},
						OrganizationId: org.Identity.ID,
					}, nil)
			},
		},
		{
			name: "expired",
			user: suite.users.Kopatych,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.InvitationByCode{
						Invitation: &organizationmanager.Invitation{
							Id:               "invite-expired-id",
							Status:           organizationmanager.Invitation_PENDING,
							ServiceUri:       "https://sourcecraft.dev/" + org.Slug,
							InviterSubjectId: suite.users.Barash.Identity.ID,
							NotAfter:         timestamppb.New(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)),
						},
						OrganizationId: org.Identity.ID,
					}, nil)
			},
			wantCode: codes.ResourceExhausted,
		},
		{
			name: "anonymous",
			user: entities.NewAnonymousUser(),
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			wantCode: codes.Unauthenticated,
		},
		{
			name: "not_found",
			user: suite.users.Kopatych,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, status.Errorf(codes.NotFound, "Invitation 'non-existent-code' not found"))
			},
			wantCode: codes.NotFound,
		},
		{
			name: "empty_invite",
			user: suite.users.Kopatych,
			request: &pb.GetInviteByCodeRequest{
				InviteCode: "code",
			},
			setupMock: func(cloudMock *mocks.MockYCPSDKInvitationClient) {
				cloudMock.EXPECT().GetByCode(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.InvitationByCode{}, nil)
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "empty_request",
			user:     suite.users.Kopatych,
			request:  &pb.GetInviteByCodeRequest{},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl, reporter := testutils.NewMockController(t)
			defer reporter.Finish(ctrl)

			if tc.setupMock != nil {
				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			resp, err := client.GetInviteByCode(ctx, tc.request)

			yarequire.ProtoStatusEqual(t, tc.wantCode, err)
			if tc.wantCode != codes.OK {
				return
			}

			// yarequire.ProtoDumpFixture(t, resp)
			yarequire.ProtoCompareWithFixture(t, resp)
		})
	}
}
