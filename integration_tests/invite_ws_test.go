package integrationtests

import (
	"common/testutils/assertjson"
	"fmt"
	"gitcore/internal/entities"
	"github.com/stretchr/testify/require"
	"testing"
)

func (suite *RwApiTestSuite) countWsMessages(ch string, message string) int {
	cnt := 0
	for _, r := range suite.WebSocketRequests() {
		if r.Channel == ch {
			err := assertjson.CheckMatch(r.Message, message)
			if err == nil {
				cnt++
			}
		}
	}
	return cnt
}

func (suite *RwApiTestSuite) requireHasWsMessage(t *testing.T, ch string, message string) {
	require.GreaterOrEqualf(t, suite.countWsMessages(ch, message), 1, "no matching messages in channel %s", ch)
}

func (suite *RwApiTestSuite) requireHasWsMessageTypes(t *testing.T, ch string, msgTypes ...entities.WsEntityType) {
	for _, msgType := range msgTypes {
		suite.requireHasWsMessage(t, ch, fmt.Sprintf(`{
			"identity": {"type": "%s"}
		}`, string(msgType)))
	}
}

// todo: OO-4839 update test to work with cloud invites
//func (suite *RwApiTestSuite) TestInviteWSNotification() {
//	t := suite.T()
//
//	var (
//		invitee = suite.users.Pikachu
//		inviter = suite.users.Slowpoke
//		org     = suite.orgs.Yandex
//	)
//
//	t.Run("Create invite", func(t *testing.T) {
//		require.NoError(t, suite.ClearWebSocketRequests())
//		suite.createInvite(t, invitee, inviter, org)
//
//		suite.requireHasWsMessage(t, fmt.Sprintf("me_%d", invitee.ID), fmt.Sprintf(`{
//				"identity": {
//					"type": "%s",
//					"userID": "%d"
//				},
//				"initiatorID": "%d"
//			}`,
//			entities.WsEntityTypes.OrganizationInvites, invitee.ID, inviter.ID),
//		)
//	})
//}

//func (suite *RwApiTestSuite) createInvite(t *testing.T, invitee, inviter *entities.User, org *entities.Organization) *schemas.Invite {
//	t.Helper()
//
//	suite.addOrgRole(t, inviter, org, iam.Roles.OrganizationManagerAdmin)
//	invite := schemas.Invite{}
//	client := suite.client.As(inviter.Identity)
//	resp, err := client.
//		SetBody(&schemas.CreateInviteRequest{Invitee: invitee.Identity}).
//		SetResult(&invite).
//		Post(fmt.Sprintf("api/v1/orgs/%s/invites", org.Slug))
//	require.NoError(t, err)
//	require.Equal(t, http.StatusCreated, resp.StatusCode(), string(resp.Body()))
//
//	return &invite
//}
