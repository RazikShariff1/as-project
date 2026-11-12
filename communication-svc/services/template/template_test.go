package template

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/communication-svc/common/apperr"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/stores"
)

func setup(t *testing.T) (*Service, *stores.MockTemplate, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	m := stores.NewMockTemplate(ctrl)

	return New(m), m, &gofr.Context{}
}

func valid() *models.TemplateRequest {
	return &models.TemplateRequest{Name: " customer/shipped ", Subject: " Shipped ", Body: "<p>Hi {{name}}</p>"}
}

func TestCreate_FillsDefaultsAndTrims(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().Create(ctx, &models.TemplateRequest{
		Name: "customer/shipped", Language: "en", Subject: "Shipped", Body: "<p>Hi {{name}}</p>", Status: "ENABLED",
	}).Return(models.Template{ID: 1}, nil)

	_, err := svc.Create(ctx, valid())
	assert.NoError(t, err)
}

func TestCreate_Rejects(t *testing.T) {
	svc, _, ctx := setup(t)

	for field, mutate := range map[string]func(*models.TemplateRequest){
		"name":     func(r *models.TemplateRequest) { r.Name = "no-template" },
		"language": func(r *models.TemplateRequest) { r.Language = "english!" },
		"subject":  func(r *models.TemplateRequest) { r.Subject = "a\nb" },
		"body":     func(r *models.TemplateRequest) { r.Body = " " },
		"status":   func(r *models.TemplateRequest) { r.Status = "maybe" },
	} {
		req := valid()
		mutate(req)

		_, err := svc.Create(ctx, req)
		assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{field}}, err, field)
	}
}

func TestCreate_DuplicateIsAConflict(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().Create(ctx, gomock.Any()).Return(models.Template{}, apperr.ErrConflict)

	_, err := svc.Create(ctx, valid())
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err)
}

func TestGet_NotFound(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().Get(ctx, int64(9)).Return(models.Template{}, apperr.ErrNotFound)

	_, err := svc.Get(ctx, 9)
	require.Error(t, err)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "template", Value: "9"}, err)
}
