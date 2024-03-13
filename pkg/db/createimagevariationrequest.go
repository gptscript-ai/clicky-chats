package db

import (
	"github.com/acorn-io/z"
	"github.com/gptscript-ai/clicky-chats/pkg/generated/openai"
	oapitypes "github.com/oapi-codegen/runtime/types"
)

type CreateImageVariationRequest struct {
	JobRequest `json:",inline"`

	Image          []byte  `json:"image"`
	Model          *string `json:"model"`
	N              *int    `json:"n"`
	ResponseFormat *string `json:"response_format"`
	Size           *string `json:"size"`
	User           *string `json:"user,omitempty"`
}

func (*CreateImageVariationRequest) IDPrefix() string {
	return "imagevariation-"
}

func (c *CreateImageVariationRequest) ToPublic() any {
	if c == nil {
		return nil
	}

	model := new(openai.CreateImageVariationRequest_Model)
	if err := model.FromCreateImageVariationRequestModel0(z.Dereference(c.Model)); err != nil {
		return nil
	}

	image := new(oapitypes.File)
	if len(c.Image) > 0 {
		image.InitFromBytes(c.Image, "image")
	}

	//nolint:govet
	return &openai.CreateImageVariationRequest{
		*image,
		model,
		c.N,
		(*openai.CreateImageVariationRequestResponseFormat)(c.ResponseFormat),
		(*openai.CreateImageVariationRequestSize)(c.Size),
		c.User,
	}
}

func (c *CreateImageVariationRequest) FromPublic(obj any) error {
	o, ok := obj.(*openai.CreateImageVariationRequest)
	if !ok {
		return InvalidTypeError{Expected: o, Got: obj}
	}

	if o == nil || c == nil {
		return nil
	}

	var model *string
	if o.Model != nil {
		m, err := o.Model.AsCreateImageVariationRequestModel0()
		if err != nil {
			return err
		}
		model = &m
	}

	image, err := o.Image.Bytes()
	if err != nil {
		return err
	}

	//nolint:govet
	*c = CreateImageVariationRequest{
		JobRequest{},
		image,
		model,
		o.N,
		(*string)(o.ResponseFormat),
		(*string)(o.Size),
		o.User,
	}

	return nil
}
