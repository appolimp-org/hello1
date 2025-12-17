package integrationtests

import (
	"bytes"
	"common/functools"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/testutils"
	"io"
	"mime/multipart"
	"os"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestAttachmentUpload() {
	t := suite.T()
	identity := suite.users.Kopatych.Identity

	type AttachmentTest struct {
		name               string
		scope              entities.AttachmentScope
		fileName           string
		expectedStatusCode int
		expectedMimeType   string
		expectedFileType   entities.AttachmentFileType
	}

	tests := []*AttachmentTest{{
		name:               "upload avatar",
		scope:              entities.AttachmentScopes.UserAvatar,
		fileName:           "cat.jpeg",
		expectedStatusCode: 200,
		expectedMimeType:   "image/jpeg",
		expectedFileType:   entities.AttachmentFileTypes.Image,
	}, {
		name:               "upload avatar with invalid format",
		scope:              entities.AttachmentScopes.UserAvatar,
		fileName:           "document.docx",
		expectedStatusCode: 400,
	}, {
		name:               "upload avatar with large size",
		scope:              entities.AttachmentScopes.UserAvatar,
		fileName:           "bigimage.jpg",
		expectedStatusCode: 400,
	}, {
		name:               "upload docx issueAttachment",
		scope:              entities.AttachmentScopes.IssueAttachment,
		fileName:           "document.docx",
		expectedStatusCode: 200,
		expectedMimeType:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		expectedFileType:   entities.AttachmentFileTypes.Document,
	}, {
		name:               "upload video issueAttachment",
		scope:              entities.AttachmentScopes.IssueAttachment,
		fileName:           "video.mp4",
		expectedStatusCode: 200,
		expectedMimeType:   "video/mp4",
		expectedFileType:   entities.AttachmentFileTypes.Video,
	}, {
		name:               "upload image issueAttachment",
		scope:              entities.AttachmentScopes.IssueAttachment,
		fileName:           "bigimage.jpg",
		expectedStatusCode: 200,
		expectedMimeType:   "image/jpeg",
		expectedFileType:   entities.AttachmentFileTypes.Image,
	}, {
		name:               "upload issueCommentAttachment",
		scope:              entities.AttachmentScopes.IssueCommentAttachment,
		fileName:           "document.pdf",
		expectedStatusCode: 200,
		expectedMimeType:   "application/pdf",
		expectedFileType:   entities.AttachmentFileTypes.Document,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedStatusCode == 200 {
				result := suite.UploadAttachment(identity, tt.fileName, tt.scope)
				require.NotEmpty(t, result.Key)

				attachmentID, err := grpc_marshalling.IDDirect(result.ID)
				require.NoError(t, err)

				attachment, err := suite.AttachmentRepo.Get(context.Background(), attachmentID)
				require.NoError(t, err)
				require.Equal(t, tt.expectedMimeType, attachment.MimeType)
				require.Equal(t, tt.expectedFileType, attachment.FileType)
			} else {
				var result interface{}
				suite.uploadAttachment(t, testutils.UserIdentities.Kopatych, tt.fileName, tt.scope, result, tt.expectedStatusCode)
			}
		})
	}

	// Parallel uploadings must not return LockingFailed error and code 400 instead of 200
	t.Run("parallel uploading", func(t *testing.T) {
		filteredTests := functools.Filter(tests, func(tt *AttachmentTest) bool {
			return tt.expectedStatusCode == 200
		})

		var wg sync.WaitGroup
		wg.Add(len(filteredTests))
		for _, tt := range filteredTests {
			go func(wg *sync.WaitGroup) {
				defer wg.Done()

				result := suite.UploadAttachment(identity, tt.fileName, tt.scope)
				require.NotEmpty(t, result.Key)

				attachmentID, err := grpc_marshalling.IDDirect(result.ID)
				require.NoError(t, err)

				attachment, err := suite.AttachmentRepo.Get(context.Background(), attachmentID)
				require.NoError(t, err)
				require.Equal(t, tt.expectedMimeType, attachment.MimeType)
				require.Equal(t, tt.expectedFileType, attachment.FileType)
			}(&wg)
		}
		wg.Wait()
	})
}

func (suite *RwApiTestSuite) UploadAttachment(
	identity entities.UserIdentity,
	fileName string,
	scope entities.AttachmentScope,
) *schemas.UploadAttachmentResponse {
	var result schemas.UploadAttachmentResponse
	suite.uploadAttachment(suite.T(), identity, fileName, scope, &result, 200)
	return &result
}

func prepareAttachmentForUpload(t *testing.T, fileName string) (bytes.Buffer, string) {
	file, err := os.Open(testutils.GetAttachmentPath(fileName))
	require.NoError(t, err)
	defer func(file *os.File) {
		if err = file.Close(); err != nil {
			t.Error(err)
		}
	}(file)

	buf := bytes.Buffer{}
	writer := multipart.NewWriter(&buf)
	fileWriter, err := writer.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = io.Copy(fileWriter, file)
	require.NoError(t, err)
	err = writer.Close()
	require.NoError(t, err)

	return buf, writer.FormDataContentType()
}

func (suite *RwApiTestSuite) uploadAttachment(
	t *testing.T,
	identity entities.UserIdentity,
	fileName string,
	scope entities.AttachmentScope,
	result any,
	expectedStatusCode int,
) {
	buf, contentType := prepareAttachmentForUpload(t, fileName)

	resp, err := suite.client.As(identity).
		SetQueryParams(map[string]string{
			"scope": string(scope),
		}).
		SetHeaders(map[string]string{
			"Content-Type": contentType,
		}).
		SetBody(buf.Bytes()).
		SetResult(&result).
		Post("/api/v1/attachments")
	require.NoError(t, err)
	require.Equal(t, expectedStatusCode, resp.StatusCode())
}

func (suite *RwApiTestSuite) uploadReleaseAttachment(
	t *testing.T,
	identity entities.UserIdentity,
	fileName string,
	repo *entities.Repository,
	result any,
	expectedStatusCode int,
) {
	buf, contentType := prepareAttachmentForUpload(t, fileName)

	resp, err := suite.client.As(identity).
		SetQueryParams(map[string]string{
			"repo_id": repo.UUID.String(),
		}).
		SetHeaders(map[string]string{
			"Content-Type": contentType,
		}).
		SetBody(buf.Bytes()).
		SetResult(&result).
		Post("/api/v1/attachments/releaseAssetUpload")
	require.NoError(t, err)
	require.Equal(t, expectedStatusCode, resp.StatusCode())
}

func (suite *RwApiTestSuite) GetAttachmentUpload(
	identity entities.UserIdentity,
	attachID string,
	filename string,
) *resty.Response {
	t := suite.T()
	resp, err := suite.client.As(identity).
		SetPathParam("id", attachID).
		Get("/api/v1/attachments/{id}")
	require.NoError(t, err)
	if resp.StatusCode() == 200 {
		require.Equal(t, fmt.Sprintf("attachment; filename=%s", filename), resp.Header().Get(echo.HeaderContentDisposition))
	}
	return resp
}
