package api

import (
	"asset-server/generated/api"
	"asset-server/internal/storage"
	"errors"
	"slices"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	storage.RegisterValidators()
	m.Run()
}

func TestStorageObjectValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		obj := api.StorageObject{
			ContentLength: 132,
			ContentType:   "application/javascript",
			Extension:     "js",
			MD5Hash:       "d41d8cd98f00b204e9800998ecf8427e",
			Path:          "bundles/asset.js",
		}
		assert.NoError(t, binding.Validator.ValidateStruct(&obj))
	})

	t.Run("invalid content type", func(t *testing.T) {
		obj := api.StorageObject{
			ContentLength: 132,
			ContentType:   "",
			Extension:     "js",
			MD5Hash:       "d41d8cd98f00b204e9800998ecf8427e",
			Path:          "bundles/asset.js",
		}
		err := binding.Validator.ValidateStruct(&obj)
		var validationErrs validator.ValidationErrors
		assert.True(t, errors.As(err, &validationErrs))
		assert.Len(t, validationErrs, 1)
		assert.Equal(t, validationErrs[0].Field(), "ContentType")
	})
}

func TestPrepareUpdateParamsValidation(t *testing.T) {
	t.Run("invalid file metadata", func(t *testing.T) {
		obj := api.PrepareUpdateBody{
			FileMetadata: []api.StorageObject{
				{
					ContentLength: 132,
					ContentType:   "",
					Extension:     "js",
					MD5Hash:       "d41d8cd98f00b204e9800998ecf8427e",
					Path:          "bundles/asset.js",
				},
			},
		}

		err := binding.Validator.ValidateStruct(&obj)
		var validationErrs validator.ValidationErrors
		assert.True(t, errors.As(err, &validationErrs))

		assert.True(t, slices.ContainsFunc(validationErrs, func(err validator.FieldError) bool {
			return err.Field() == "ContentType"
		}))
	})
}
