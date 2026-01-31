package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/a-gierczak/paratrooper/generated/api"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type HTTPError struct {
	StatusCode int
	Message    string
	Inner      error
}

func (e *HTTPError) Error() string {
	if e.Inner != nil {
		return fmt.Sprintf("%s: %s", e.Message, e.Inner.Error())
	}

	return e.Message
}

func (e *HTTPError) Unwrap() error {
	return e.Inner
}

func NewNotFoundError(message string) *HTTPError {
	return &HTTPError{
		StatusCode: http.StatusNotFound,
		Message:    message,
	}
}

type ValidationError struct {
	Field   string
	Message string
}

func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field %s: %s", e.Field, e.Message)
}

func NewErrorHandlingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		validationErrorResponse := api.ValidationErrorJSONResponse{
			Errors: make([]api.ValidationFieldError, 0),
		}

		for _, err := range c.Errors {
			var apiValidationError *ValidationError
			var validatorErrors validator.ValidationErrors
			var httpError *HTTPError

			if errors.As(err.Err, &validatorErrors) {
				for _, fieldError := range validatorErrors {
					validationErrorResponse.Errors = append(
						validationErrorResponse.Errors,
						api.ValidationFieldError{
							Field:   fieldError.Field(),
							Message: fieldError.Error(),
						},
					)
				}
				continue
			}

			if errors.As(err.Err, &apiValidationError) {
				validationErrorResponse.Errors = append(
					validationErrorResponse.Errors,
					api.ValidationFieldError{
						Field:   apiValidationError.Field,
						Message: apiValidationError.Message,
					},
				)
				continue
			}

			if errors.As(err.Err, &httpError) {
				c.AbortWithStatusJSON(
					httpError.StatusCode,
					api.GenericError{
						Error: httpError.Message,
					},
				)
				return
			}

			c.AbortWithStatusJSON(
				http.StatusInternalServerError,
				api.InternalServerErrorJSONResponse{Error: err.Error()},
			)
			return
		}

		// all errors are validation errors
		c.AbortWithStatusJSON(
			http.StatusBadRequest,
			validationErrorResponse,
		)
	}
}

func NewValidationErrorResponse(field, message string) struct {
	api.ValidationErrorJSONResponse
} {
	return struct {
		api.ValidationErrorJSONResponse
	}{
		api.ValidationErrorJSONResponse{
			Errors: []api.ValidationFieldError{
				{
					Field:   field,
					Message: message,
				},
			},
		},
	}
}
