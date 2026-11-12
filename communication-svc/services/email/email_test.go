package email

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/communication-svc/common/apperr"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/sender"
	"github.com/zopping/communication-svc/stores"
)

type fakeSender struct {
	sent []*sender.Message
	err  error
}

func (f *fakeSender) Send(_ context.Context, m *sender.Message) error {
	if f.err != nil {
		return f.err
	}

	f.sent = append(f.sent, m)

	return nil
}

func setup(t *testing.T) (*Service, *stores.MockEmail, *stores.MockTemplate, *fakeSender, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	emails, templates, out := stores.NewMockEmail(ctrl), stores.NewMockTemplate(ctrl), &fakeSender{}

	return New(emails, templates, out, 3), emails, templates, out, &gofr.Context{Context: context.Background()}
}

var welcome = models.Template{Name: "customer/welcome", Language: "en", Subject: "Welcome, {{name}}!", Body: "<p>Hi {{name}}</p>", Status: models.TemplateEnabled}

func TestSend_RendersAndQueues(t *testing.T) {
	svc, emails, templates, _, ctx := setup(t)

	templates.EXPECT().Find(ctx, "customer/welcome", "en").Return(welcome, nil)
	emails.EXPECT().Enqueue(ctx, &models.Outbound{
		Type: "customer/welcome", Language: "en", To: "asha@example.com,b@example.com",
		Subject: "Welcome, Asha <3!", Body: "<p>Hi Asha &lt;3</p>",
	}).Return(models.Email{ID: 7, Status: models.StatusQueued}, nil)

	resp, err := svc.Send(ctx, &models.SendRequest{
		To: "Asha <asha@example.com>, b@example.com", Type: "customer/welcome", Data: map[string]any{"name": "Asha <3"},
	})
	require.NoError(t, err)
	assert.Equal(t, models.SendResponse{ID: 7, Status: "queued", Message: queuedMessage}, resp)
}

func TestSend_FallsBackToEnglish(t *testing.T) {
	svc, emails, templates, _, ctx := setup(t)

	templates.EXPECT().Find(ctx, "customer/welcome", "fr").Return(models.Template{}, apperr.ErrNotFound)
	templates.EXPECT().Find(ctx, "customer/welcome", "en").Return(welcome, nil)
	emails.EXPECT().Enqueue(ctx, gomock.Any()).Return(models.Email{ID: 1, Status: "queued"}, nil)

	_, err := svc.Send(ctx, &models.SendRequest{To: "a@example.com", Type: "customer/welcome", Language: "FR", Data: map[string]any{"name": "A"}})
	assert.NoError(t, err)
}

func TestSend_DisabledTemplateIsSkipped(t *testing.T) {
	svc, _, templates, _, ctx := setup(t)

	off := welcome
	off.Status = models.TemplateDisabled
	templates.EXPECT().Find(ctx, "customer/welcome", "en").Return(off, nil)

	resp, err := svc.Send(ctx, &models.SendRequest{To: "a@example.com", Type: "customer/welcome", Data: map[string]any{"name": "A"}})
	require.NoError(t, err)
	assert.Equal(t, "skipped", resp.Status)
}

func TestSend_NoTemplate(t *testing.T) {
	svc, emails, _, _, ctx := setup(t)

	emails.EXPECT().Enqueue(ctx, &models.Outbound{
		Type: "no-template", Language: "en", To: "a@example.com", Subject: "Hello  Bcc: evil@example.com", Body: "<b>x</b>",
	}).Return(models.Email{ID: 2, Status: "queued"}, nil)

	_, err := svc.Send(ctx, &models.SendRequest{
		To: "a@example.com", Type: "no-template", Data: map[string]any{"subject": "Hello\r\nBcc: evil@example.com", "body": "<b>x</b>"},
	})
	assert.NoError(t, err, "a line break in the subject cannot add a header")
}

func TestSend_Rejects(t *testing.T) {
	svc, _, templates, _, ctx := setup(t)

	templates.EXPECT().Find(ctx, "nope", "en").Return(models.Template{}, apperr.ErrNotFound)
	templates.EXPECT().Find(ctx, "customer/welcome", "en").Return(welcome, nil)

	tests := []struct {
		name string
		req  models.SendRequest
		want string
	}{
		{"no type", models.SendRequest{To: "a@example.com"}, "type"},
		{"no recipient", models.SendRequest{Type: "x"}, "to"},
		{"bad recipient", models.SendRequest{Type: "x", To: "not-an-email"}, "to"},
		{"bad cc", models.SendRequest{Type: "x", To: "a@example.com", Cc: "nope"}, "cc"},
	}

	for _, tc := range tests {
		_, err := svc.Send(ctx, &tc.req)
		assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{tc.want}}, err, tc.name)
	}

	_, err := svc.Send(ctx, &models.SendRequest{To: "a@example.com", Type: "nope"})
	assert.ErrorContains(t, err, "No template found")

	_, err = svc.Send(ctx, &models.SendRequest{To: "a@example.com", Type: "customer/welcome", Data: map[string]any{}})
	assert.ErrorContains(t, err, "missing field `name`")
}

func TestDeliver(t *testing.T) {
	due := []models.Outbound{
		{ID: 1, To: "a@example.com,b@example.com", Cc: "c@example.com", Subject: "s", Body: "b", Attempts: 1},
	}

	t.Run("sends and marks sent", func(t *testing.T) {
		svc, emails, _, out, ctx := setup(t)

		emails.EXPECT().Claim(ctx, batchSize, claimSeconds).Return(due, nil)
		emails.EXPECT().MarkSent(ctx, int64(1)).Return(nil)

		sent, failed, err := svc.Deliver(ctx)
		require.NoError(t, err)
		assert.Equal(t, [2]int{1, 0}, [2]int{sent, failed})
		assert.Equal(t, []string{"a@example.com", "b@example.com"}, out.sent[0].To)
		assert.Equal(t, []string{"c@example.com"}, out.sent[0].Cc)
	})

	t.Run("a failure is retried later with a back-off", func(t *testing.T) {
		svc, emails, _, out, ctx := setup(t)
		out.err = errors.New("smtp down")

		emails.EXPECT().Claim(ctx, batchSize, claimSeconds).Return(due, nil)
		emails.EXPECT().MarkRetry(ctx, int64(1), 30, "smtp down").Return(nil)

		sent, failed, err := svc.Deliver(ctx)
		require.NoError(t, err)
		assert.Equal(t, [2]int{0, 1}, [2]int{sent, failed})
	})

	t.Run("out of attempts is failed for good", func(t *testing.T) {
		svc, emails, _, out, ctx := setup(t)
		out.err = errors.New("smtp down")

		last := []models.Outbound{{ID: 1, To: "a@example.com", Attempts: 3}}
		emails.EXPECT().Claim(ctx, batchSize, claimSeconds).Return(last, nil)
		emails.EXPECT().MarkFailed(ctx, int64(1), "smtp down").Return(nil)

		_, failed, err := svc.Deliver(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, failed)
	})
}

func TestBackoff(t *testing.T) {
	assert.Equal(t, []int{30, 60, 120, 240}, []int{backoff(1), backoff(2), backoff(3), backoff(4)})
	assert.Equal(t, maxBackoff, backoff(50))
}
