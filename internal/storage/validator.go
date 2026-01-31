package storage

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

const MaxObjectSize = 100 * 1024 * 1024 // 100MB

func RegisterValidators() error {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return errors.New("failed to get validator")
	}

	if err := v.RegisterValidation("max_object_size", validateMaxObjectSize); err != nil {
		return fmt.Errorf("failed to register max_object_size validator: %w", err)
	}

	if err := v.RegisterValidation("asset_path", validateAssetPath); err != nil {
		return fmt.Errorf("failed to register asset_path validator: %w", err)
	}

	if err := v.RegisterValidation("asset_ext", validateAssetExt); err != nil {
		return fmt.Errorf("failed to register asset_ext validator: %w", err)
	}

	return nil
}

func validateMaxObjectSize(fl validator.FieldLevel) bool {
	return fl.Field().Int() <= MaxObjectSize
}

// validateAssetPath asset path is the local path of the file.
// It's sent by the client, and is not prefixed with the project and update id
func validateAssetPath(fl validator.FieldLevel) bool {
	str := fl.Field().String()
	dir, file := path.Split(str)
	return !path.IsAbs(str) && !strings.Contains(dir, "..") && file != ""
}

var extRegex = regexp.MustCompile(`^\.[a-zA-Z0-9\.\-]+$`)

func validateAssetExt(fl validator.FieldLevel) bool {
	str := fl.Field().String()
	return extRegex.MatchString(str)
}
