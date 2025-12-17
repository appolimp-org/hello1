package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/generated/mocks"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"net/http"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"
	"time"

	organizationmanagersdk "bb.yandex-team.ru/cloud/cloud-go/api/ycpsdk/clients/organizationmanager/v1"
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/organizationmanager/v1"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (suite *RwApiTestSuite) TestPublicAPICreateOrganizationInvites() {
	t := suite.T()
	admin := suite.users.Admin

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-invites",
			Claims:     entities.OrganizationClaims{Name: "for-invites"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	userWithoutRights := suite.users.Krosh

	// Users for slug tests
	slugUser := suite.users.Barash
	failedUser := suite.users.Pikachu

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		url       string
		user      *entities.User
		request   *pbPub.CreateOrganizationInvitesBody
		setupMock func(*mocks.MockYCPSDKInvitationClient)
		expected  int
	}{
		{
			name: "happy_path",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "test@example.com",
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "test@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_org_id",
			url:  fmt.Sprintf("/orgs/by-id/%s/invites", org.UUID),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "byid@example.com",
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "byid@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "multiple_emails",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "user1@example.com",
					},
					{
						Email: "user2@example.com",
					},
					{
						Email: "user3@example.com",
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{
									{
										Status: organizationmanager.Invitation_PENDING,
										Identity: &organizationmanager.Invitation_Invitee_{
											Invitee: &organizationmanager.Invitation_Invitee{Email: "user1@example.com"},
										},
									},
									{
										Status: organizationmanager.Invitation_PENDING,
										Identity: &organizationmanager.Invitation_Invitee_{
											Invitee: &organizationmanager.Invitation_Invitee{Email: "user2@example.com"},
										},
									},
									{
										Status: organizationmanager.Invitation_PENDING,
										Identity: &organizationmanager.Invitation_Invitee_{
											Invitee: &organizationmanager.Invitation_Invitee{Email: "user3@example.com"},
										},
									},
								},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "with_ttl",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "ttl@example.com",
					},
				},
				TtlInDays: 7,
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "ttl@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "with_ttl_max",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "ttlmax@example.com",
					},
				},
				TtlInDays: 30,
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "ttlmax@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "with_ttl_min",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "ttlmin@example.com",
					},
				},
				TtlInDays: 1,
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "ttlmin@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "partial_success",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "valid@example.com"},
					{Email: "invalid@example.com"},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								ValidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "valid@example.com"},
									},
								}},
								InvalidInvitations: []*organizationmanager.Invitation{{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "invalid@example.com"},
									},
								}},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "all_failed",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "fail1@example.com"},
					{Email: "fail2@example.com"},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, in *organizationmanager.CreateInvitationsRequest, opts ...grpc.CallOption) (*organizationmanagersdk.InvitationCreateOperation, error) {
						return &organizationmanagersdk.InvitationCreateOperation{
							Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
								InvalidInvitations: []*organizationmanager.Invitation{
									{
										Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
										Identity: &organizationmanager.Invitation_Invitee_{
											Invitee: &organizationmanager.Invitation_Invitee{Email: "fail1@example.com"},
										},
									},
									{
										Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
										Identity: &organizationmanager.Invitation_Invitee_{
											Invitee: &organizationmanager.Invitation_Invitee{Email: "fail2@example.com"},
										},
									},
								},
							}),
						}, nil
					})
			},
			expected: http.StatusAccepted,
		},
		{
			name: "org_not_found",
			url:  "/orgs/nonexistent-org/invites",
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "test@example.com",
					},
				},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "org_not_found_by_id",
			url:  "/orgs/by-id/00000000-0000-0000-0000-000000000000/invites",
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "test@example.com",
					},
				},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "forbidden",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: userWithoutRights,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "test@example.com",
					},
				},
			},
			expected: http.StatusForbidden,
		},
		{
			name: "invalid_email",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "not-an-email",
					},
				},
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "empty_invitees",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{},
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "ttl_too_large",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "not-an-email",
					},
				},
				TtlInDays: 31,
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "by_slug_happy_path",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Slug: slugUser.Username,
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{{
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_UserAccount_{
									UserAccount: &organizationmanager.Invitation_UserAccount{
										Id: slugUser.Identity.ID,
									},
								},
							}},
						}),
					}, nil)
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_org_member",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Slug: orgMember.Username,
					},
				},
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_federative_user",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Slug: fedUser.Username,
					},
				},
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_iam_error",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Slug: failedUser.Username,
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							InvalidInvitations: []*organizationmanager.Invitation{{
								Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
								Identity: &organizationmanager.Invitation_UserAccount_{
									UserAccount: &organizationmanager.Invitation_UserAccount{
										Id: failedUser.Identity.ID,
									},
								},
							}},
						}),
					}, nil)
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_user_not_found",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Slug: "nonexistent-user-slug",
					},
				},
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_comprehensive",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			request: &pbPub.CreateOrganizationInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Slug: slugUser.Username},   // invited successfully
					{Slug: failedUser.Username}, // IAM error
					{Slug: orgMember.Username},  // already org member - error
					{Slug: fedUser.Username},    // federative - error
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: slugUser.Identity.ID,
										},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil)
			},
			expected: http.StatusAccepted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			var resp *resty.Response
			var err error
			t.Run("operation", func(t *testing.T) {
				resp, err = suite.gwClient.As(tc.user.Identity).
					SetBody(tc.request).
					Post(tc.url)

				require.NoError(t, err)
				yarequire.StatusCode(t, resp, err, tc.expected)

				if tc.expected == http.StatusAccepted {
					operationID, err := yarequire.GetStringFromJSON(resp.Body(), "operation_id")
					require.NoError(t, err)
					require.NotEmpty(t, operationID)
				}

				// yarequire.HTTPDumpFixture(t, resp)
				yarequire.HTTPCompareWithFixture(t, resp,
					"**/operation_id",
					"**/created_at",
					"**/modified_at",
					"**/status_url",
					"**/request_id",
				)
			})

			if tc.expected != http.StatusAccepted {
				return
			}

			t.Run("result", func(t *testing.T) {
				require.NotNil(t, resp)

				statusURL, err := yarequire.GetStringFromJSON(resp.Body(), "status_url")
				require.NoError(t, err)
				require.NotEmpty(t, statusURL)

				require.Eventuallyf(t, func() bool {
					resp, err = suite.gwClient.As(tc.user.Identity).
						Get(statusURL)
					require.NoError(t, err)
					yarequire.StatusCode(t, resp, err, http.StatusOK)

					status, err := yarequire.GetStringFromJSON(resp.Body(), "status")
					require.NoError(t, err)

					return status == "success" || status == "failed"
				}, time.Second*20, 50*time.Millisecond, "operation not finished",
				)

				// yarequire.HTTPDumpFixture(t, resp)
				yarequire.HTTPCompareWithFixture(t, resp,
					"**/operation_id",
					"**/status_url",
					"**/created_at",
					"**/modified_at",
					"**/expires_at",
				)
			})
		})
	}
}

func (suite *RwApiTestSuite) TestPublicAPIListOrganizationInvites() {
	t := suite.T()
	admin := suite.users.Admin
	org := suite.orgs.Yandex42

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	userWithoutRights := suite.users.Krosh

	for _, tc := range []struct {
		name      string
		url       string
		user      *entities.User
		params    map[string]string
		setupMock func(*mocks.MockYCPSDKInvitationClient)
		expected  int
	}{
		{
			name: "happy_path",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.ListOrganizationInvitationsResponse{
						Invitations: []*organizationmanager.Invitation{
							{
								Id:     "invite-1",
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_Invitee_{
									Invitee: &organizationmanager.Invitation_Invitee{
										Email: "user1@example.com",
									},
								},
							},
							{
								Id:     "invite-2",
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_UserAccount_{
									UserAccount: &organizationmanager.Invitation_UserAccount{
										Id: suite.users.Krosh.Identity.ID,
									},
								},
							},
						},
						NextPageToken: "",
					}, nil)
			},
			expected: http.StatusOK,
		},
		{
			name: "by_org_id",
			url:  fmt.Sprintf("/orgs/by-id/%s/invites", org.UUID.String()),
			user: admin,
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.ListOrganizationInvitationsResponse{
						Invitations: []*organizationmanager.Invitation{
							{
								Id:     "invite-byid",
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_Invitee_{
									Invitee: &organizationmanager.Invitation_Invitee{
										Email: "byid@example.com",
									},
								},
							},
						},
						NextPageToken: "",
					}, nil)
			},
			expected: http.StatusOK,
		},
		{
			name: "empty_list",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.ListOrganizationInvitationsResponse{
						Invitations:   []*organizationmanager.Invitation{},
						NextPageToken: "",
					}, nil)
			},
			expected: http.StatusOK,
		},
		{
			name: "with_pagination",
			url:  fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user: admin,
			params: map[string]string{
				"page_size": "10",
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					ListForOrganization(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanager.ListOrganizationInvitationsResponse{
						Invitations: []*organizationmanager.Invitation{
							{
								Id:     "invite-page1",
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_Invitee_{
									Invitee: &organizationmanager.Invitation_Invitee{
										Email: "page@example.com",
									},
								},
							},
						},
						NextPageToken: "next-page-token",
					}, nil)
			},
			expected: http.StatusOK,
		},
		{
			name:     "org_not_found",
			url:      "/orgs/nonexistent-org/invites",
			user:     admin,
			expected: http.StatusNotFound,
		},
		{
			name:     "org_not_found_by_id",
			url:      "/orgs/by-id/00000000-0000-0000-0000-000000000000/invites",
			user:     admin,
			expected: http.StatusNotFound,
		},
		{
			name:     "forbidden",
			url:      fmt.Sprintf("/orgs/%s/invites", org.Slug),
			user:     userWithoutRights,
			expected: http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			req := suite.gwClient.As(tc.user.Identity)
			for k, v := range tc.params {
				req = req.SetQueryParam(k, v)
			}

			resp, err := req.Get(tc.url)
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp,
				"**/request_id",
				"**/created_at",
				"**/expires_at",
			)
		})
	}
}

func (suite *RwApiTestSuite) TestPublicAPIGetCreateOrganizationInvitesOperation() {
	t := suite.T()
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	otherOrg := suite.orgs.Yandex

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	userWithoutRights := suite.users.Krosh

	for _, tc := range []struct {
		name      string
		url       string
		user      *entities.User
		operation *entities.Operation
		status    entities.OperationStatus
		response  *pb.InternalCreateBulkInvitesOperationResponse
		opErr     error
		expected  int
	}{
		{
			name: "success",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "success_____________"),
			operation: &entities.Operation{
				ID:        "success_____________",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-1",
						InviteeEmail: "test@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "success_by_org_id",
			user: admin,
			url:  fmt.Sprintf("/orgs/by-id/%s/operations/create-invites/%s", org.UUID.String(), "success_by_org_id___"),
			operation: &entities.Operation{
				ID:        "success_by_org_id___",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-byid",
						InviteeEmail: "byid@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-byid-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "partial_success",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "partial_success_____"),
			operation: &entities.Operation{
				ID:        "partial_success_____",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-valid",
						InviteeEmail: "valid@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-valid-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
				Errors: []*pb.InviteError{
					{
						InviteeEmail: "invalid@example.com",
						Error:        except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
					{
						InviteeId: suite.users.Kopatych.Identity.ID,
						Error:     except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "all_failed",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "all_failed__________"),
			operation: &entities.Operation{
				ID:        "all_failed__________",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				Errors: []*pb.InviteError{
					{
						InviteeEmail: "fail1@example.com",
						Error:        except.InviteeHasAlreadyJoined.BuildNoStack().Message.Proto(),
					},
					{
						InviteeEmail: "fail2@example.com",
						Error:        except.InviteeHasAlreadyJoined.BuildNoStack().Message.Proto(),
					},
					{
						InviteeId: suite.users.Kopatych.Identity.ID,
						Error:     except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "failed_operation",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "failed_operation____"),
			operation: &entities.Operation{
				ID:        "failed_operation____",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			opErr:    except.InternalError.Build("some error"),
			expected: http.StatusOK,
		},
		{
			name: "in_progress",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "in_progress_________"),
			operation: &entities.Operation{
				ID:        "in_progress_________",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.InProgress,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			expected: http.StatusOK,
		},
		{
			name:     "operation_wrong_id",
			user:     admin,
			url:      fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "123"),
			expected: http.StatusBadRequest,
		},
		{
			name:     "operation_not_found",
			user:     admin,
			url:      fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "operation_not_found_"),
			expected: http.StatusNotFound,
		},
		{
			name: "operation_wrong_type",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "operation_wrong_type"),
			operation: &entities.Operation{
				ID:     "operation_wrong_type",
				Type:   entities.OperationTypes.Stub,
				Status: entities.OperationStatuses.Success,
				UserID: admin.ID,
				IamObject: entities.IAMObject{
					Type: entities.ObjectTypes.Organization,
					ID:   org.ID,
				},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "operation_wrong_org",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "operation_wrong_org_"),
			operation: &entities.Operation{
				ID:        "operation_wrong_org_",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: otherOrg.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{{
					Id:           "invite-other",
					InviteeEmail: "other@example.com",
					Status:       pb.Invite_PENDING,
				}},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "forbidden",
			user: userWithoutRights,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", org.Slug, "forbidden___________"),
			operation: &entities.Operation{
				ID:        "forbidden___________",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{{
					Id:           "invite-forbidden",
					InviteeEmail: "forbidden@example.com",
					Status:       pb.Invite_PENDING,
				}},
			},
			expected: http.StatusForbidden,
		},
		{
			name: "org_not_found",
			user: admin,
			url:  fmt.Sprintf("/orgs/%s/operations/create-invites/%s", "nonexistent-org", "org_not_found_______"),
			operation: &entities.Operation{
				ID:        "org_not_found_______",
				Type:      entities.OperationTypes.CreateInvitesBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: org.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{{
					Id:           "invite-orgnotfound",
					InviteeEmail: "orgnotfound@example.com",
					Status:       pb.Invite_PENDING,
				}},
			},
			expected: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.operation != nil {
				if tc.response != nil {
					require.NoError(t, tc.operation.SetResponse(tc.response))
				}
				if tc.opErr != nil {
					require.NoError(t, tc.operation.SetError(tc.opErr))
				}
				_, err := suite.OpRepo.Create(context.Background(), tc.operation)
				require.NoError(t, err)
			}

			resp, err := suite.gwClient.As(tc.user.Identity).Get(tc.url)
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp,
				"**/operation_id",
				"**/status_url",
				"**/created_at",
				"**/modified_at",
				"**/expires_at",
				"**/request_id",
			)
		})
	}
}

func (suite *RwApiTestSuite) TestPublicAPICreateRepositoryInvites() {
	t := suite.T()
	admin := suite.users.Admin

	_, orgID, err := suite.Params.OrgService.CreateOrganization(
		context.Background(),
		nil,
		suite.users.Admin,
		entities.IdentityProviders.IAM,
		entities.Organization{
			Slug:       "for-repo-invites",
			Claims:     entities.OrganizationClaims{Name: "for-repo-invites"},
			Visibility: entities.Visibilities.Public,
		},
	)
	require.NoError(t, err)
	org, err := suite.Params.OrgRepo.GetOrganizationByID(context.Background(), orgID)
	require.NoError(t, err)

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	repo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		OrgID:      org.ID,
		Slug:       "test-repo",
		Visibility: entities.Visibilities.Public,
	})

	// Setup test users
	userWithoutRights := suite.users.Krosh
	slugUser := suite.users.Barash
	failedUser := suite.users.Pikachu

	orgMember := suite.users.Kopatych
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, orgMember.Identity)
	require.NoError(t, err)

	fedUser := suite.users.PinInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUser.ID)
	require.NoError(t, err)

	fedUserMember := suite.users.BiBiInternal
	_, err = suite.Pool.Exec(context.Background(), `update users set federation_id = 'ya-team' where id = $1;`, fedUserMember.ID)
	require.NoError(t, err)
	err = suite.OrgService.AddUser(context.Background(), nil, org.Identity, fedUserMember.Identity)
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		url           string
		user          *entities.User
		request       *pbPub.CreateRepositoryInvitesBody
		setupMock     func(*mocks.MockYCPSDKInvitationClient)
		setupBindings func()
		expected      int
	}{
		{
			name: "by_repo_id",
			url:  fmt.Sprintf("/repos/by-id/%s/invites", repo.UUID),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{
						Email: "byid@example.com",
					},
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{{
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_Invitee_{
									Invitee: &organizationmanager.Invitation_Invitee{Email: "byid@example.com"},
								},
							}},
						}),
					}, nil)
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_email",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "user1@example.com"},
					{Email: "user2@example.com"},
					{Email: "user3@example.com"},
					{Email: "invalid@example.com"},
				},
				Role: pbPub.RepoRole_developer.Enum(),
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "user1@example.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "user2@example.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "user3@example.com"},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "invalid@example.com"},
									},
								},
							},
						}),
					}, nil)
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Slug: slugUser.Username},      // invited successfully
					{Slug: failedUser.Username},    // IAM error
					{Slug: orgMember.Username},     // already org member - auto-added
					{Slug: fedUserMember.Username}, // already org member - auto-added
					{Slug: fedUser.Username},       // federative - error
				},
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_Invitee_{
										Invitee: &organizationmanager.Invitation_Invitee{Email: "valid@example.com"},
									},
								},
								{
									Status: organizationmanager.Invitation_PENDING,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: slugUser.Identity.ID,
										},
									},
								},
							},
							InvalidInvitations: []*organizationmanager.Invitation{
								{
									Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
									Identity: &organizationmanager.Invitation_UserAccount_{
										UserAccount: &organizationmanager.Invitation_UserAccount{
											Id: failedUser.Identity.ID,
										},
									},
								},
							},
						}),
					}, nil).AnyTimes()
			},
			expected: http.StatusAccepted,
		},
		{
			name: "binding_error",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Slug: slugUser.Username},   // invited successfully but binding will fail
					{Slug: failedUser.Username}, // IAM error
					{Slug: orgMember.Username},  // auto-add but binding will fail
				},
			},
			setupBindings: func() {
				suite.accessBindingsService.SetForbiddenSubjectTypes([]entities.SubjectType{entities.Subjects.User})
			},
			setupMock: func(m *mocks.MockYCPSDKInvitationClient) {
				m.EXPECT().
					Create(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationCreateOperation{
						Operation: suite.prepareSDKResponse(&organizationmanager.CreateInvitationsResponse{
							ValidInvitations: []*organizationmanager.Invitation{{
								Status: organizationmanager.Invitation_PENDING,
								Identity: &organizationmanager.Invitation_UserAccount_{
									UserAccount: &organizationmanager.Invitation_UserAccount{
										Id: slugUser.Identity.ID,
									},
								},
							}},
							InvalidInvitations: []*organizationmanager.Invitation{{
								Status: organizationmanager.Invitation_STATUS_UNSPECIFIED,
								Identity: &organizationmanager.Invitation_UserAccount_{
									UserAccount: &organizationmanager.Invitation_UserAccount{
										Id: failedUser.Identity.ID,
									},
								},
							}},
						}),
					}, nil)

				m.EXPECT().
					Delete(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&organizationmanagersdk.InvitationDeleteOperation{
						Operation: suite.prepareSDKResponse(&emptypb.Empty{}),
					}, nil).
					AnyTimes()
			},
			expected: http.StatusAccepted,
		},
		{
			name: "by_slug_user_not_found",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Slug: "nonexistent-user-slug"},
				},
			},
			expected: http.StatusAccepted,
		},
		{
			name: "empty_invitees",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{},
			},
			expected: http.StatusBadRequest,
		},
		{
			name: "repo_not_found",
			url:  "/repos/nonexistent-org/nonexistent-repo/invites",
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "test@example.com"},
				},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "repo_not_found_by_id",
			url:  "/repos/by-id/00000000-0000-0000-0000-000000000000/invites",
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "test@example.com"},
				},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "forbidden",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: userWithoutRights,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "test@example.com"},
				},
			},
			expected: http.StatusForbidden,
		},
		{
			name: "invalid_email",
			url:  fmt.Sprintf("/repos/%s/invites", repo.FullSlug()),
			user: admin,
			request: &pbPub.CreateRepositoryInvitesBody{
				Invitees: []*pbPub.InviteeInput{
					{Email: "not-an-email"},
				},
				TtlInDays: 100,
			},
			expected: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupMock != nil {
				ctrl, reporter := testutils.NewMockController(t)
				defer reporter.Finish(ctrl)

				cloudMock := mocks.NewMockYCPSDKInvitationClient(ctrl)
				suite.Params.InvitationService.(interfaces.InvitationClientReplacer).ReplaceClient(cloudMock)
				tc.setupMock(cloudMock)
			}

			if tc.setupBindings != nil {
				tc.setupBindings()
				defer suite.accessBindingsService.SetForbiddenSubjectTypes(nil)
			}

			var resp *resty.Response
			var err error
			t.Run("operation", func(t *testing.T) {
				resp, err = suite.gwClient.As(tc.user.Identity).
					SetBody(tc.request).
					Post(tc.url)

				require.NoError(t, err)
				yarequire.StatusCode(t, resp, err, tc.expected)

				if tc.expected == http.StatusAccepted {
					operationID, err := yarequire.GetStringFromJSON(resp.Body(), "operation_id")
					require.NoError(t, err)
					require.NotEmpty(t, operationID)
				}

				// yarequire.HTTPDumpFixture(t, resp)
				yarequire.HTTPCompareWithFixture(t, resp,
					"**/operation_id",
					"**/created_at",
					"**/modified_at",
					"**/status_url",
					"**/request_id",
				)
			})

			if tc.expected != http.StatusAccepted {
				return
			}

			t.Run("result", func(t *testing.T) {
				require.NotNil(t, resp)

				statusURL, err := yarequire.GetStringFromJSON(resp.Body(), "status_url")
				require.NoError(t, err)
				require.NotEmpty(t, statusURL)

				require.Eventuallyf(t, func() bool {
					resp, err = suite.gwClient.As(tc.user.Identity).
						Get(statusURL)
					require.NoError(t, err)
					yarequire.StatusCode(t, resp, err, http.StatusOK)

					status, err := yarequire.GetStringFromJSON(resp.Body(), "status")
					require.NoError(t, err)

					return status == "success" || status == "failed"
				}, time.Second*20, 50*time.Millisecond, "operation not finished",
				)

				// yarequire.HTTPDumpFixture(t, resp)
				yarequire.HTTPCompareWithFixture(t, resp,
					"**/operation_id",
					"**/status_url",
					"**/created_at",
					"**/modified_at",
					"**/expires_at",
				)
			})
		})
	}
}

func (suite *RwApiTestSuite) TestPublicAPIGetCreateRepositoryInvitesOperation() {
	t := suite.T()
	admin := suite.users.Admin
	org := suite.orgs.Yandex42
	otherOrg := suite.orgs.Yandex

	suite.addExternalOrgRole(t, admin, org, iam.Roles.Admin)
	suite.addExternalOrgRole(t, admin, org, iam.Roles.OrganizationManagerOrganizationsOwner)

	userWithoutRights := suite.users.Krosh

	// Create a repository for testing
	repo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		Slug:  "test-repo-invites",
		OrgID: org.ID,
	})
	otherRepo := suite.makeRepo(admin, &interfaces.CreateRepositoryArgs{
		Slug:  "other-repo-invites",
		OrgID: otherOrg.ID,
	})

	for _, tc := range []struct {
		name      string
		url       string
		user      *entities.User
		operation *entities.Operation
		response  *pb.InternalCreateBulkInvitesOperationResponse
		opErr     error
		expected  int
	}{
		{
			name: "success",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "success_____________"),
			operation: &entities.Operation{
				ID:        "success_____________",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-1",
						InviteeEmail: "test@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "success_by_repo_id",
			user: admin,
			url:  fmt.Sprintf("/repos/by-id/%s/operations/create-invites/%s", repo.UUID.String(), "success_by_repo_id__"),
			operation: &entities.Operation{
				ID:        "success_by_repo_id__",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-byid",
						InviteeEmail: "byid@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-byid-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "partial_success",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "partial_success_____"),
			operation: &entities.Operation{
				ID:        "partial_success_____",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{
					{
						Id:           "invite-valid",
						InviteeEmail: "valid@example.com",
						Status:       pb.Invite_PENDING,
					},
					{
						Id:        "invite-valid-2",
						InviteeId: suite.users.Krosh.Identity.ID,
						Status:    pb.Invite_PENDING,
					},
				},
				Errors: []*pb.InviteError{
					{
						InviteeEmail: "invalid@example.com",
						Error:        except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
					{
						InviteeId: suite.users.Kopatych.Identity.ID,
						Error:     except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "all_failed",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "all_failed__________"),
			operation: &entities.Operation{
				ID:        "all_failed__________",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				Errors: []*pb.InviteError{
					{
						InviteeEmail: "fail1@example.com",
						Error:        except.InviteeHasAlreadyJoined.BuildNoStack().Message.Proto(),
					},
					{
						InviteeEmail: "fail2@example.com",
						Error:        except.InviteeHasAlreadyJoined.BuildNoStack().Message.Proto(),
					},
					{
						InviteeId: suite.users.Kopatych.Identity.ID,
						Error:     except.InviteAlreadyCreated.BuildNoStack().Message.Proto(),
					},
				},
			},
			expected: http.StatusOK,
		},
		{
			name: "failed_operation",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "failed_operation____"),
			operation: &entities.Operation{
				ID:        "failed_operation____",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			opErr:    except.InternalError.Build("some error"),
			expected: http.StatusOK,
		},
		{
			name: "in_progress",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "in_progress_________"),
			operation: &entities.Operation{
				ID:        "in_progress_________",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.InProgress,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			expected: http.StatusOK,
		},
		{
			name:     "operation_wrong_id",
			user:     admin,
			url:      fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "123"),
			expected: http.StatusBadRequest,
		},
		{
			name:     "operation_not_found",
			user:     admin,
			url:      fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "operation_not_found_"),
			expected: http.StatusNotFound,
		},
		{
			name: "operation_wrong_type",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "operation_wrong_type"),
			operation: &entities.Operation{
				ID:        "operation_wrong_type",
				Type:      entities.OperationTypes.Stub,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			expected: http.StatusNotFound,
		},
		{
			name: "operation_wrong_repo",
			user: admin,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "operation_wrong_repo"),
			operation: &entities.Operation{
				ID:        "operation_wrong_repo",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: otherRepo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{{
					Id:           "invite-other",
					InviteeEmail: "other@example.com",
					Status:       pb.Invite_PENDING,
				}},
			},
			expected: http.StatusNotFound,
		},
		{
			name: "forbidden",
			user: userWithoutRights,
			url:  fmt.Sprintf("/repos/%s/operations/create-invites/%s", repo.FullSlug(), "forbidden___________"),
			operation: &entities.Operation{
				ID:        "forbidden___________",
				Type:      entities.OperationTypes.AddToRepoBulk,
				Status:    entities.OperationStatuses.Success,
				UserID:    admin.ID,
				IamObject: repo.Object(),
			},
			response: &pb.InternalCreateBulkInvitesOperationResponse{
				CreatedInvites: []*pb.Invite{{
					Id:           "invite-forbidden",
					InviteeEmail: "forbidden@example.com",
					Status:       pb.Invite_PENDING,
				}},
			},
			expected: http.StatusForbidden,
		},
		{
			name:     "repo_not_found",
			user:     admin,
			url:      fmt.Sprintf("/repos/%s/operations/create-invites/%s", "nonexistent-repo", "repo_not_found______"),
			expected: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.operation != nil {
				if tc.response != nil {
					require.NoError(t, tc.operation.SetResponse(tc.response))
				}
				if tc.opErr != nil {
					require.NoError(t, tc.operation.SetError(tc.opErr))
				}
				_, err := suite.OpRepo.Create(context.Background(), tc.operation)
				require.NoError(t, err)
			}

			resp, err := suite.gwClient.As(tc.user.Identity).Get(tc.url)
			require.NoError(t, err)
			yarequire.StatusCode(t, resp, err, tc.expected)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp,
				"**/operation_id",
				"**/status_url",
				"**/created_at",
				"**/modified_at",
				"**/expires_at",
				"**/request_id",
			)
		})
	}
}
