package integrationtests

import (
	"bytes"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type attachmentSubtest func(suite *RwApiTestSuite, t *testing.T, data attachmentSubtestData)

type attachmentSubtestData struct {
	name, url, urlNoAccess, urlNotAuthor string
	urlDeleted                           string
	scope                                entities.AttachmentScope
	tests                                []attachmentSubtest
	urlIDOR                              string
}

func (suite *RwApiTestSuite) TestPublicAPIAttachmentCollection() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:         2000,
		RepoID:     repoID,
		Title:      "Issue0",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})

	issueDifferentAuthor := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         2001,
		RepoID:     repoID,
		Title:      "Issue1",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})

	issueIDOR := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         999,
		RepoID:     repoID,
		Title:      "Issue IDOR",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})

	issuePrivate := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         2003,
		RepoID:     repoID,
		Title:      "Issue1",
		Visibility: entities.IssueVisibilities.Private,
		LabelIDs:   []uint64{},
	})

	issueComment := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{IssueID: &issueDifferentAuthor.ID, Body: "xxxx"})
	issueCommentNotAuthor := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{IssueID: &issueDifferentAuthor.ID, Body: "xxxx"})
	issueCommentPrivateIssue := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{IssueID: &issuePrivate.ID, Body: "xxxx"})

	attachment := suite.UploadAttachment(suite.users.Admin.Identity, "cat.jpeg", entities.AttachmentScopes.IssueCommentAttachment)
	attachmentID, err := grpc_marshalling.IDDirect(attachment.ID)
	require.NoError(t, err)

	issueCommentDeleted := suite.makeIssueComment(suite.users.Admin, &makeIssueCommentOptions{
		IssueID: &issueDifferentAuthor.ID, Body: "xxxx", AttachmentIDs: []uint64{attachmentID},
	})
	suite.IssueCommentService.Delete(context.Background(), issueCommentDeleted.ID, suite.users.Admin)

	// test cases are typical, only entity changes

	entities := []attachmentSubtestData{
		{
			name:        "issue",
			scope:       entities.AttachmentScopes.IssueAttachment,
			url:         fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID),
			urlNoAccess: fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issueDifferentAuthor.PublicID),
			urlIDOR:     fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issueIDOR.PublicID),
			tests: []attachmentSubtest{
				happyPath,
				noAccess,
			},
		},
		{
			name:         "issue comment",
			scope:        entities.AttachmentScopes.IssueCommentAttachment,
			url:          fmt.Sprintf("/issue_comments/id:%s/attachments", issueComment.UUID.String()),
			urlNoAccess:  fmt.Sprintf("/issue_comments/id:%s/attachments", issueCommentPrivateIssue.UUID.String()),
			urlNotAuthor: fmt.Sprintf("/issue_comments/id:%s/attachments", issueCommentNotAuthor.UUID.String()),
			urlDeleted:   fmt.Sprintf("/issue_comments/id:%s/attachments", issueCommentDeleted.UUID.String()),
			tests: []attachmentSubtest{
				happyPath,
				noAccess,
				differentAuthor,
				deleted,
			},
		},
	}

	for _, tc := range entities {
		for _, fn := range tc.tests {
			fullName := strings.Split(runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name(), ".")
			name := fullName[len(fullName)-1]
			t.Run(fmt.Sprintf("%s: %s", tc.name, name), func(t *testing.T) {
				fn(suite, t, tc)
			})
		}
	}

}

func prepareUploadForm(filename string) (data []byte, formContentType string, err error) {
	file, err := os.Open(testutils.GetAttachmentPath(filename))
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	buf := bytes.Buffer{}
	writer := multipart.NewWriter(&buf)

	fileWriter, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}

	_, err = io.Copy(fileWriter, file)
	if err != nil {
		return nil, "", err
	}

	err = writer.Close()
	if err != nil {
		return nil, "", err
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}

func happyPath(suite *RwApiTestSuite, t *testing.T, data attachmentSubtestData) {
	var ids []string
	for _, fn := range []string{"rabbit.jpg", "cat.jpeg", "document.pdf"} {
		raw, contentType, err := prepareUploadForm(fn)
		require.NoError(t, err)
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(data.url)

		yarequire.StatusCode(t, resp, err, http.StatusCreated)

		data, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)
		require.NotEmpty(t, data)
		ids = append(ids, data)
	}

	r, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Delete(data.url + "/" + ids[0])
	yarequire.StatusCode(t, r, err, http.StatusNoContent)

	r, err = suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Get(data.url)

	require.NoError(t, err)
	yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")

	// we deleted that attachment
	r, err = suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Get(data.url + "/" + ids[0])
	yarequire.StatusCode(t, r, err, http.StatusNotFound)

	// this must be downloadable
	r, err = suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Get(data.url + "/" + ids[1])
	yarequire.StatusCode(t, r, err, http.StatusOK)

	url, err := yarequire.GetStringFromJSON(r.Body(), "url")
	require.NoError(t, err)
	file, err := http.Get(url)
	require.NoError(t, err)
	defer file.Body.Close()

	downloadedData, err := io.ReadAll(file.Body)
	require.NoError(t, err)
	file2, err := os.Open(testutils.GetAttachmentPath("cat.jpeg"))
	require.NoError(t, err)
	existingData, err := io.ReadAll(file2)
	require.NoError(t, err)
	defer file2.Close()

	require.Equal(t, downloadedData, existingData)

	// no idors please
	r, err = suite.gwClient.
		As(suite.users.Kopatych.Identity).
		Get(data.urlIDOR + "/" + ids[1])
	yarequire.StatusCode(t, r, err, http.StatusNotFound)
}

func noAccess(suite *RwApiTestSuite, t *testing.T, data attachmentSubtestData) {
	raw, contentType, err := prepareUploadForm("cat.jpeg")
	require.NoError(t, err)
	resp, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		SetHeader("Content-Type", contentType).
		SetBody(raw).
		Post(data.urlNoAccess)

	yarequire.StatusCode(t, resp, err, http.StatusForbidden)
}

func differentAuthor(suite *RwApiTestSuite, t *testing.T, data attachmentSubtestData) {
	raw, contentType, err := prepareUploadForm("cat.jpeg")
	require.NoError(t, err)
	resp, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		SetHeader("Content-Type", contentType).
		SetBody(raw).
		Post(data.urlNotAuthor)

	yarequire.StatusCode(t, resp, err, http.StatusForbidden)
}

func deleted(suite *RwApiTestSuite, t *testing.T, data attachmentSubtestData) {
	r, err := suite.gwClient.
		As(suite.users.Admin.Identity).
		Get(data.urlDeleted)

	yarequire.StatusCode(t, r, err, http.StatusNotFound)

	raw, contentType, err := prepareUploadForm("cat.jpeg")
	require.NoError(t, err)
	resp, err := suite.gwClient.
		As(suite.users.Admin.Identity).
		SetHeader("Content-Type", contentType).
		SetBody(raw).
		Post(data.urlDeleted)

	yarequire.StatusCode(t, resp, err, http.StatusNotFound)
}

func (suite *RwApiTestSuite) TestQuotaAbuse() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:         2000,
		RepoID:     repoID,
		Title:      "Issue0",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})
	raw, contentType, err := prepareUploadForm("cat.jpeg")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err := suite.Params.AttachmentRepo.Create(context.Background(), &entities.Attachment{
			Name:      "foo.zip",
			Size:      34 * 1024 * 1024,
			MimeType:  "le mime",
			FileType:  entities.AttachmentFileTypes.Container,
			Status:    entities.AttachmentStatuses.Uploaded,
			Scope:     entities.AttachmentScopes.IssueAttachment,
			CreatedBy: suite.users.Kopatych.ID,
			UpdatedBy: suite.users.Kopatych.ID,
		})
		require.NoError(t, err)
	}

	resp, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		SetHeader("Content-Type", contentType).
		SetBody(raw).
		Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
	yarequire.StatusCode(t, resp, err, http.StatusBadRequest)

}

func (suite *RwApiTestSuite) TestProxyAbuse() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:         2000,
		RepoID:     repoID,
		Title:      "Issue0",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})
	raw, contentType, err := prepareUploadForm("cat.jpeg")
	require.NoError(t, err)

	t.Run("corrupt form", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", contentType).
			SetBody("I am form-multipart, I swear!").
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("too much content", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("file", "snoopy.exe")
		require.NoError(t, err)
		dump := make([]byte, 1024*1024*5)
		dump[1] = 42
		_, err = fileWriter.Write(dump)
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("wrong field", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("file2", "snoopy.ico")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("hewwo"))
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})
	t.Run("bad path", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post("/issue_comments/id:111zzzzz/attachments")
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("path traversal", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("file", "../../etc/passwd")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("hewwo"))
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		data, err := yarequire.GetStringFromJSON(resp.Body(), "name")
		require.NoError(t, err)
		require.Equal(t, data, "passwd")
	})

	t.Run("json", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(`{"file": "myfile"}`).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/attachments", orgSlug, repoSlug, issue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

}

func (suite *RwApiTestSuite) TestPublicAPIReleaseAttachments() {
	t := suite.T()

	repo := suite.repos.Alpha
	orgSlug := repo.OrgSlug
	repoSlug := repo.Slug

	// Setup: Create git tags for releases
	suite.mustBash(repo, `
		git checkout -b master
		git tag v1.0.0
		touch x
		git add . && git commit -m "commit for v2.0.0"
		git tag v2.0.0
	`)

	// Create releases
	suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Repo:    repo,
		Tag:     "v1.0.0",
		Title:   "Published Release",
		Publish: utils.PtrFromValue(true),
	})

	suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Repo:    repo,
		Tag:     "v2.0.0",
		Title:   "Draft Release",
		Publish: utils.PtrFromValue(false),
	})

	t.Run("upload attachment by tag: success", func(t *testing.T) {
		raw, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusCreated)

		// Verify response contains attachment information
		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)
		require.NotEmpty(t, id)

		name, err := yarequire.GetStringFromJSON(resp.Body(), "name")
		require.NoError(t, err)
		require.Equal(t, "cat.jpeg", name)

		size, err := yarequire.GetStringFromJSON(resp.Body(), "size")
		require.NoError(t, err)
		require.NotEmpty(t, size)

		// Verify attachment appears in release assets
		getResp, err := suite.gwClient.As(suite.users.Admin.Identity).Get(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, getResp, nil, http.StatusOK)

		// Check that the attachment is present in the assets
		assets, err := yarequire.GetArrayFromJSON(getResp.Body(), "assets")
		require.NoError(t, err)
		require.NotEmpty(t, assets)

		// Find our uploaded attachment
		found := false
		for _, assetJSON := range assets {
			attachmentID, err := yarequire.GetStringFromJSON(assetJSON, "attachment.id")
			if err != nil {
				continue
			}
			assetName, err := yarequire.GetStringFromJSON(assetJSON, "name")
			if err != nil {
				continue
			}
			if attachmentID == id && assetName == "cat.jpeg" {
				found = true
				break
			}
		}
		require.True(t, found, "Uploaded attachment should appear in release assets")
	})

	t.Run("upload attachment by ID: success", func(t *testing.T) {
		// First get the release via public API to get its UUID
		getResp, err := suite.gwClient.As(suite.users.Admin.Identity).Get(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0", orgSlug, repoSlug))
		require.NoError(t, err)
		releaseUUID, err := yarequire.GetStringFromJSON(getResp.Body(), "id")
		require.NoError(t, err)

		raw, contentType, err := prepareUploadForm("rabbit.jpg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/releases/id:%s/attachments", releaseUUID))

		yarequire.StatusCode(t, resp, err, http.StatusCreated)

		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)
		require.NotEmpty(t, id)

		name, err := yarequire.GetStringFromJSON(resp.Body(), "name")
		require.NoError(t, err)
		require.Equal(t, "rabbit.jpg", name)

		// Verify attachment appears in release assets
		getResp2, err := suite.gwClient.As(suite.users.Admin.Identity).Get(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, getResp2, nil, http.StatusOK)

		// Check that the attachment is present in the assets
		assets, err := yarequire.GetArrayFromJSON(getResp2.Body(), "assets")
		require.NoError(t, err)
		require.NotEmpty(t, assets)

		// Find our uploaded attachment
		found := false
		for _, assetJSON := range assets {
			attachmentID, err := yarequire.GetStringFromJSON(assetJSON, "attachment.id")
			if err != nil {
				continue
			}
			assetName, err := yarequire.GetStringFromJSON(assetJSON, "name")
			if err != nil {
				continue
			}
			if attachmentID == id && assetName == "rabbit.jpg" {
				found = true
				break
			}
		}
		require.True(t, found, "Uploaded attachment should appear in release assets")
	})

	t.Run("upload attachment to draft release: success", func(t *testing.T) {
		// First get the release via public API to get its UUID
		getResp, err := suite.gwClient.As(suite.users.Admin.Identity).Get(fmt.Sprintf("/repos/%s/%s/releases/tag/v2.0.0", orgSlug, repoSlug))
		require.NoError(t, err)
		draftReleaseUUID, err := yarequire.GetStringFromJSON(getResp.Body(), "id")
		require.NoError(t, err)

		raw, contentType, err := prepareUploadForm("document.pdf")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/releases/id:%s/attachments", draftReleaseUUID))

		yarequire.StatusCode(t, resp, err, http.StatusCreated)

		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)
		require.NotEmpty(t, id)

		name, err := yarequire.GetStringFromJSON(resp.Body(), "name")
		require.NoError(t, err)
		require.Equal(t, "document.pdf", name)

		// Verify attachment appears in release assets
		getResp2, err := suite.gwClient.As(suite.users.Admin.Identity).Get(fmt.Sprintf("/repos/%s/%s/releases/tag/v2.0.0", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, getResp2, nil, http.StatusOK)

		// Check that the attachment is present in the assets
		assets, err := yarequire.GetArrayFromJSON(getResp2.Body(), "assets")
		require.NoError(t, err)
		require.NotEmpty(t, assets)

		// Find our uploaded attachment
		found := false
		for _, assetJSON := range assets {
			attachmentID, err := yarequire.GetStringFromJSON(assetJSON, "attachment.id")
			if err != nil {
				continue
			}
			assetName, err := yarequire.GetStringFromJSON(assetJSON, "name")
			if err != nil {
				continue
			}
			if attachmentID == id && assetName == "document.pdf" {
				found = true
				break
			}
		}
		require.True(t, found, "Uploaded attachment should appear in release assets")
	})

	t.Run("upload attachment: permission denied - viewer", func(t *testing.T) {
		raw, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.AuthViewer.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("upload attachment: release not found by tag", func(t *testing.T) {
		raw, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v999.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})

	t.Run("upload attachment: release not found by ID", func(t *testing.T) {
		raw, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		fakeUUID := "00000000-0000-0000-0000-000000000000"
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/releases/id:%s/attachments", fakeUUID))

		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})

	t.Run("upload attachment: corrupt form", func(t *testing.T) {
		_, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", contentType).
			SetBody("I am form-multipart, I swear!").
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("upload attachment: file too large", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("file", "large.bin")
		require.NoError(t, err)
		dump := make([]byte, 1024*1024*11) // 11MB - exceeds the 10MB limit in test config
		dump[1] = 42
		_, err = fileWriter.Write(dump)
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("upload attachment: wrong field name", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("attachment", "cat.jpeg") // Should be "file"
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("fake image data"))
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)
	})

	t.Run("upload attachment: path traversal prevention", func(t *testing.T) {
		buf := bytes.Buffer{}
		writer := multipart.NewWriter(&buf)

		fileWriter, err := writer.CreateFormFile("file", "../../etc/passwd")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("fake content"))
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetHeader("Content-Type", writer.FormDataContentType()).
			SetBody(buf.Bytes()).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		// Verify the filename is sanitized
		name, err := yarequire.GetStringFromJSON(resp.Body(), "name")
		require.NoError(t, err)
		require.Equal(t, "passwd", name)
	})

	t.Run("upload attachment: unauthenticated", func(t *testing.T) {
		raw, contentType, err := prepareUploadForm("cat.jpeg")
		require.NoError(t, err)

		resp, err := suite.gwClient.
			AsGuest().
			SetHeader("Content-Type", contentType).
			SetBody(raw).
			Post(fmt.Sprintf("/repos/%s/%s/releases/tag/v1.0.0/attachments", orgSlug, repoSlug))

		yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)
	})
}
