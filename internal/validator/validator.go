package validator

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type StructValidator struct {
	Validator *validator.Validate
}

func New() *StructValidator {
	v := validator.New()

	_ = v.RegisterValidation("slug", func(fl validator.FieldLevel) bool {
		return slugRegex.MatchString(fl.Field().String())
	})

	return &StructValidator{
		Validator: v,
	}
}

func (v *StructValidator) Validate(out any) error {
	return v.Validator.Struct(out)
}
