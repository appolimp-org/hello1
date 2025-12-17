package integrationtests

import (
	"common/audit"
	"context"
	"encoding/json"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"gitcore/internal/interfaces"

	cloud_events "bb.yandex-team.ru/cloud/cloud-go/genproto/publicapi/yandex/cloud/events"
	audit_events "bb.yandex-team.ru/cloud/cloud-go/genproto/publicapi/yandex/cloud/events/sourcecraft"
)

type AuditEventTest struct {
	EventsRepo interfaces.AuditEventsRepository
}

func (a *AuditEventTest) ProtoCompareOpts() []cmp.Option {
	return []cmp.Option{
		protocmp.IgnoreFields(&cloud_events.OrganizationEventMetadata{}, "event_id", "created_at", "organization_id", "tracing_context"),
		protocmp.IgnoreFields(&cloud_events.TracingContext{}, "trace_id", "span_id"),
		protocmp.IgnoreFields(&cloud_events.RequestMetadata{}, "request_id"),
		protocmp.IgnoreFields(&cloud_events.Response{}, "operation_id"),
		protocmp.IgnoreFields(&status.Status{}, "message"),
		protocmp.IgnoreFields(&cloud_events.RequestedPermissions{}, "resource_id"),
		protocmp.IgnoreFields(&audit_events.RepositoryEventDetails{}, "repository_id", "repository_slug"),
		protocmp.IgnoreFields(&audit_events.DeleteRepository_RequestParameters{}, "repository_id"),
		protocmp.IgnoreFields(&audit_events.UpdateRepository_RequestParameters{}, "repository_id"),
		protocmp.IgnoreFields(&audit_events.OrganizationEventDetails{}, "organization_id", "organization_slug"),
		protocmp.IgnoreFields(&audit_events.OnboardOrganization_RequestParameters{}, "organization_slug"),
		protocmp.IgnoreFields(&audit_events.UpdateOrganization_RequestParameters{}, "organization_id", "organization_slug"),
		protocmp.IgnoreFields(&audit_events.OffboardOrganization_RequestParameters{}, "organization_id"),
		protocmp.IgnoreFields(&audit_events.OnboardCloudRegistry_EventDetails{}, "registry_id"),
		protocmp.IgnoreFields(&audit_events.OffboardCloudRegistry_EventDetails{}, "registry_id"),
		protocmp.IgnoreFields(&audit_events.OffboardCloudRegistry_RequestParameters{}, "registry_id"),
		protocmp.IgnoreFields(&audit_events.PersonalPublicSshKeyEventDetails{}, "key_id"),
		protocmp.IgnoreFields(&audit_events.RemovePersonalPublicSshKey_RequestParameters{}, "key_id"),
		protocmp.IgnoreFields(&audit_events.PersonalAccessTokenEventDetails{}, "key_id", "name"),
		protocmp.IgnoreFields(&audit_events.UpdatePersonalAccessToken_RequestParameters{}, "key_id"),
		protocmp.IgnoreFields(&audit_events.DeletePersonalAccessToken_RequestParameters{}, "key_id"),
	}
}

func (a *AuditEventTest) DeleteAuditEvents(ctx context.Context) error {
	events, err := a.EventsRepo.ListAuditEvents(ctx)
	if err != nil {
		return err
	}
	_, err = a.EventsRepo.DeleteBulk(ctx, events)
	return err
}

func (a *AuditEventTest) GetCreateRepositoryAuditEvents(ctx context.Context) ([]*audit_events.CreateRepository, error) {
	return getAuditEvents[audit_events.CreateRepository](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetDeleteRepositoryAuditEvents(ctx context.Context) ([]*audit_events.DeleteRepository, error) {
	return getAuditEvents[audit_events.DeleteRepository](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetUpdateRepositoryAuditEvents(ctx context.Context) ([]*audit_events.UpdateRepository, error) {
	return getAuditEvents[audit_events.UpdateRepository](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetOnboardOrganizationAuditEvents(ctx context.Context) ([]*audit_events.OnboardOrganization, error) {
	return getAuditEvents[audit_events.OnboardOrganization](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetOffboardOrganizationAuditEvents(ctx context.Context) ([]*audit_events.OffboardOrganization, error) {
	return getAuditEvents[audit_events.OffboardOrganization](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetUpdateOrganizationAuditEvents(ctx context.Context) ([]*audit_events.UpdateOrganization, error) {
	return getAuditEvents[audit_events.UpdateOrganization](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetOnboardCloudRegistryAuditEvents(ctx context.Context) ([]*audit_events.OnboardCloudRegistry, error) {
	return getAuditEvents[audit_events.OnboardCloudRegistry](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetOffboardCloudRegistryAuditEvents(ctx context.Context) ([]*audit_events.OffboardCloudRegistry, error) {
	return getAuditEvents[audit_events.OffboardCloudRegistry](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetUpdateRepositoryAccessBindingsAuditEvents(ctx context.Context) ([]*audit_events.UpdateRepositoryAccessBindings, error) {
	return getAuditEvents[audit_events.UpdateRepositoryAccessBindings](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetAddPersonalPublicSSHKeyAuditEvents(ctx context.Context) ([]*audit_events.AddPersonalPublicSshKey, error) {
	return getAuditEvents[audit_events.AddPersonalPublicSshKey](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetRemovePersonalPublicSSHKeyAuditEvents(ctx context.Context) ([]*audit_events.RemovePersonalPublicSshKey, error) {
	return getAuditEvents[audit_events.RemovePersonalPublicSshKey](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetCreatePersonalAccessTokenAuditEvents(ctx context.Context) ([]*audit_events.CreatePersonalAccessToken, error) {
	return getAuditEvents[audit_events.CreatePersonalAccessToken](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetUpdatePersonalAccessTokenAuditEvents(ctx context.Context) ([]*audit_events.UpdatePersonalAccessToken, error) {
	return getAuditEvents[audit_events.UpdatePersonalAccessToken](ctx, a.EventsRepo)
}

func (a *AuditEventTest) GetDeletePersonalAccessTokenAuditEvents(ctx context.Context) ([]*audit_events.DeletePersonalAccessToken, error) {
	return getAuditEvents[audit_events.DeletePersonalAccessToken](ctx, a.EventsRepo)
}

func (a *AuditEventTest) HasEventMetaDataOrganizationID(t *testing.T, eventMsg audit.EventMessage) {
	t.Helper()
	md := eventMsg.GetEventMetadata()
	require.True(t, len(md.OrganizationId) > 0)
}

func auditEventTest(eventsRepo interfaces.AuditEventsRepository) *AuditEventTest {
	return &AuditEventTest{
		EventsRepo: eventsRepo,
	}
}

func getSpecificAuditEvents(ctx context.Context, eventsRepo interfaces.AuditEventsRepository, createMsg func() any) ([]any, error) {
	events, err := eventsRepo.ListAuditEvents(ctx)
	if err != nil {
		return nil, err
	}
	res := []any{}
	for _, event := range events {
		data := event.Payload
		metadata := make(map[string][]byte)
		err = json.Unmarshal(event.Metadata, &metadata)
		if err != nil {
			return nil, err
		}
		msg := createMsg()
		protoMsg, ok := msg.(proto.Message)
		if !ok {
			continue
		}
		var err error
		_, err = audit.DeserializeEvent(data, metadata, protoMsg)
		if err == nil {
			res = append(res, protoMsg)
			continue
		}
	}
	return res, nil
}

func getAuditEvents[T any](ctx context.Context, eventsRepo interfaces.AuditEventsRepository) ([]*T, error) {
	events, err := getSpecificAuditEvents(ctx, eventsRepo, func() any {
		return new(T)
	})
	if err != nil {
		return nil, err
	}
	res := []*T{}
	for _, event := range events {
		createRepoEvent, ok := event.(*T)
		if ok {
			res = append(res, createRepoEvent)
		} else {
			return nil, errors.New("event is not of type T")
		}
	}
	return res, nil
}
