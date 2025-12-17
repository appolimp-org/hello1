package integrationtests

import (
	"private_api/generated/yandex/cloud/public/sourcecraft/v1/events"
	"strconv"

	"gitcore/internal/entities"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_SingleBranch() {
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
    push:
      - hooks: [wh1]
`)

	ts.Enabled = true
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 1)
	require.Equal(t, "commit file1\n", payload.Commits[0].Message)
	require.False(t, payload.HasMoreCommits, "should not have more commits when exactly at limit")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_RefsUpdateLimit() {
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
    push:
      - hooks: [wh1]
`)

	ts.Enabled = true

	// Update master
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit1")

	suite.makeNewFileWithContent(wt.Cgit, "file2.txt", "file2")
	wt.Cgit.Must(t, "add", "*")
	wt.Cgit.Must(t, "commit", "-m", "commit2")

	for i := range 10 {
		branchName := "branch" + strconv.Itoa(i)
		wt.Cgit.Must(t, "checkout", "-b", branchName)
		suite.makeNewFileWithContent(wt.Cgit, branchName+".txt", branchName)
		wt.Cgit.Must(t, "add", "*")
		wt.Cgit.Must(t, "commit", "-m", "commit on "+branchName)
	}

	// Push all branches at once to trigger multiple ref updates
	wt.Cgit.Must(t, "push", "origin", "--all")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 2)
	require.Equal(t, "commit2\n", payload.Commits[0].Message)
	require.Equal(t, "commit1\n", payload.Commits[1].Message)
	require.False(t, payload.HasMoreCommits, "should not have more commits when exactly at limit")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_CommitsLimit() {
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
    push:
      - hooks: [wh1]
`)

	ts.Enabled = true

	// Update master
	for i := 1; i <= 20; i++ {
		fileName := "file" + strconv.Itoa(i)
		suite.makeNewFileWithContent(wt.Cgit, fileName+".txt", fileName)
		wt.Cgit.Must(t, "add", "*")
		wt.Cgit.Must(t, "commit", "-m", "commit_"+fileName)
	}

	// Push all branches at once to trigger multiple ref updates
	wt.Cgit.Must(t, "push", "origin", "--all")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 20)
	require.False(t, payload.HasMoreCommits, "should not have more commits when exactly at limit")
	for i := 20; i > 0; i-- {
		fileName := "file" + strconv.Itoa(i)
		require.Equal(t, "commit_"+fileName+"\n", payload.Commits[20-i].Message)
	}
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_CommitsExceedLimit() {
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
    push:
      - hooks: [wh1]
`)

	ts.Enabled = true

	// Create more commits than the limit (20)
	for i := 1; i <= 25; i++ {
		fileName := "file" + strconv.Itoa(i)
		suite.makeNewFileWithContent(wt.Cgit, fileName+".txt", fileName)
		wt.Cgit.Must(t, "add", "*")
		wt.Cgit.Must(t, "commit", "-m", "commit_"+fileName)
	}

	// Push all commits
	wt.Cgit.Must(t, "push", "origin", "--all")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push

	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 20, "should return exactly 20 commits (the limit)")
	require.True(t, payload.HasMoreCommits, "should indicate there are more commits beyond the limit")

	// Verify we got the 20 most recent commits (file25 down to file6)
	for i := 25; i > 5; i-- {
		fileName := "file" + strconv.Itoa(i)
		require.Equal(t, "commit_"+fileName+"\n", payload.Commits[25-i].Message)
	}
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_FullEventName() {
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
    repository.push:
      - hooks: [wh1]
`)

	ts.Enabled = true
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/master", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 1)
	require.Equal(t, "commit file1\n", payload.Commits[0].Message)
	require.False(t, payload.HasMoreCommits, "should not have more commits when exactly at limit")
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_AnnotatedTag() {
	t := suite.T()

	ts := suite.createTestWebhookServer()
	defer ts.Close()

	wt := suite.prepareWebhooksTestWithConfig(`
webhooks:
  hooks:
    - slug: wh1
      url: "` + ts.Server.URL + `"
      active: true
  on:
    push:
      - hooks: [wh1]
`)

	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "master")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	ts.Enabled = true
	wt.Cgit.Must(t, "tag", "-a", "myTag", "-m", "myTag message")
	wt.Cgit.Must(t, "push", "origin", "--tags")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/tags/myTag", payload.RefUpdate.Ref)
	require.NotEmpty(t, payload.Commits)
	require.Equal(t, 1, len(payload.Commits))
}

func (suite *RwApiTestSuite) TestWebhooksHandler_Push_CreateBranch() {
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
    push:
      - hooks: [wh1]
`)

	ts.Enabled = true

	wt.Cgit.Must(t, "branch", "new_branch")
	wt.Cgit.Must(t, "checkout", "new_branch")
	suite.makeNewFileWithContent(wt.Cgit, "file1.txt", "file1")
	suite.commitAll(wt.Cgit, "commit file1", "new_branch")
	suite.WaitForWorkflows(t, entities.WorkflowTypes.WebhookPost)

	require.Len(t, ts.Deliveries, 1, "should be single delivery")
	var payload events.Push
	require.NoError(t, protojson.Unmarshal(ts.Deliveries[0].Body, &payload))
	require.Equal(t, "repository.push", payload.Header.Type)
	require.Equal(t, "refs/heads/new_branch", payload.RefUpdate.Ref)
	require.Len(t, payload.Commits, 1)
	require.Equal(t, "commit file1\n", payload.Commits[0].Message)
}
