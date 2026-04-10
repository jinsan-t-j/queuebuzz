package middlewares

import (
	"errors"
	"fmt"
	"sync"

	"queuebuzz/internal/config"
	"queuebuzz/internal/exceptions"
	"queuebuzz/internal/log"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
)

type ErrorHandler struct {
	cfg *config.Config
}

type ErrorResponse struct {
	Message string `json:"message"`
	Error   any    `json:"error,omitempty"`
}

var (
	errorHandlerInstance *ErrorHandler
	errorHandlerOnce     sync.Once
)

const (
	unmarshalTypeError = "Invalid Type"
	syntaxError        = "Invalid JSON Syntax"
	validationError    = "Validation Failed"
	genericError       = "Something went wrong"
)

func NewErrorHandler(cfg *config.Config) *ErrorHandler {
	errorHandlerOnce.Do(func() {
		errorHandlerInstance = &ErrorHandler{
			cfg: cfg,
		}
	})

	return errorHandlerInstance
}

func (e *ErrorHandler) Handle(c fiber.Ctx, err error) error {
	var statusCode int
	var message string
	var details interface{}

	isProduction := e.cfg.IsProduction()

	var unmarshalErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	var validationErrors validator.ValidationErrors
	var fiberError *fiber.Error
	var fieldEx *exceptions.FieldException
	var appEx *exceptions.AppException

	switch {
	case errors.As(err, &fieldEx):
		statusCode = fieldEx.Code
		message = validationError
		details = map[string]string{
			fieldEx.Field: fieldEx.Message,
		}

	case errors.As(err, &appEx):
		statusCode = appEx.Code
		message = appEx.Message

	case errors.As(err, &unmarshalErr):
		statusCode = fiber.StatusBadRequest
		message = unmarshalTypeError
		details = map[string]string{
			unmarshalErr.Field: fmt.Sprintf("Expected %s", unmarshalErr.Type),
		}

	case errors.As(err, &syntaxErr):
		statusCode = fiber.StatusBadRequest
		message = syntaxError

	case errors.As(err, &validationErrors):
		statusCode = fiber.StatusBadRequest
		message = validationError
		details = formatValidationErrors(validationErrors)

	case errors.As(err, &fiberError):
		statusCode = fiberError.Code
		message = fiberError.Message

		if statusCode >= fiber.StatusInternalServerError {
			log.Error().
				Err(fiberError).
				Str("method", c.Method()).
				Str("path", c.Path()).
				Int("status", statusCode).
				Msg("Fiber Server Error")
		} else {
			log.Info().
				Err(fiberError).
				Str("method", c.Method()).
				Str("path", c.Path()).
				Int("status", statusCode).
				Msg("Fiber Client Error")
		}

	default:
		statusCode = fiber.StatusInternalServerError
		message = genericError

		log.Error().
			Err(err).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Msg("Unhandled Server Error")
	}

	if isProduction {
		if statusCode >= 500 {
			message = genericError
			details = nil
		}
	}

	return c.Status(statusCode).JSON(ErrorResponse{
		Message: message,
		Error:   details,
	})
}

func formatValidationErrors(errs validator.ValidationErrors) map[string]string {
	result := make(map[string]string)
	for _, err := range errs {
		result[err.Field()] = msgForTag(err.Tag())
	}
	return result
}

func msgForTag(tag string) string {
	switch tag {
	case "required":
		return "This field is required"
	case "email":
		return "Invalid email format"
	case "gte":
		return "Value is too low"
	case "lte":
		return "Value is too high"
	case "max":
		return "Value exceeds maximum length"
	case "min":
		return "Value is below minimum length"
	case "oneof":
		return "Invalid value"
	default:
		return "Invalid value"
	}
}
