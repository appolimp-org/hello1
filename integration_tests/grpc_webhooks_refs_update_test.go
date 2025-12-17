package integrationtests

import (
	"private_api/generated/yandex/cloud/public/sourcecraft/v1/events"

	"gitcore/internal/entities"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func (suite *RwApiTestSuite) TestWebhooksHandler_RefsUpdate_SingleBranch() {
	t := suite.T()

	ts := suite.createTestWebhookServer()
	defer ts.Close()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Refs Update Webhook"
      url: "` + ts.Server.URL + `"
      active: true
  on:
    refs_update:
      - hooks: [wh1]
`)

	ts.Enabled = true
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.RefsUpdate
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.refs_update", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdates[0].Ref)
}

func (suite *RwApiTestSuite) TestWebhooksHandler_RefsUpdate_FullEventName() {
	t := suite.T()

	ts := suite.createTestWebhookServer()
	defer ts.Close()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      name: "Refs Update Webhook"
      url: "` + ts.Server.URL + `"
      active: true
  on:
    repository.refs_update:
      - hooks: [wh1]
`)

	ts.Enabled = true
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.RefsUpdate
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.refs_update", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdates[0].Ref)
}
